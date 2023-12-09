package automod

import (
	"context"
	"fmt"
	"log/slog"
	"strings"

	comatproto "github.com/bluesky-social/indigo/api/atproto"
	appbsky "github.com/bluesky-social/indigo/api/bsky"
)

type ModReport struct {
	ReasonType string
	Comment    string
}

type CounterRef struct {
	Name   string
	Val    string
	Period *string
}

type CounterDistinctRef struct {
	Name   string
	Bucket string
	Val    string
}

// base type for events specific to an account, usually derived from a repo event stream message (one such message may result in multiple `RepoEvent`)
//
// events are both containers for data about the event itself (similar to an HTTP request type); aggregate results and state (counters, mod actions) to be persisted after all rules are run; and act as an API for additional network reads and operations.
type RepoEvent struct {
	Engine                    *Engine
	Err                       error
	Logger                    *slog.Logger
	Account                   AccountMeta
	CounterIncrements         []CounterRef
	CounterDistinctIncrements []CounterDistinctRef // TODO: better variable names
	AccountLabels             []string
	AccountFlags              []string
	AccountReports            []ModReport
	AccountTakedown           bool
}

func (e *RepoEvent) GetCount(name, val, period string) int {
	v, err := e.Engine.GetCount(name, val, period)
	if err != nil {
		e.Err = err
		return 0
	}
	return v
}

func (e *RepoEvent) Increment(name, val string) {
	e.CounterIncrements = append(e.CounterIncrements, CounterRef{Name: name, Val: val})
}

func (e *RepoEvent) IncrementPeriod(name, val string, period string) {
	e.CounterIncrements = append(e.CounterIncrements, CounterRef{Name: name, Val: val, Period: &period})
}

func (e *RepoEvent) GetCountDistinct(name, bucket, period string) int {
	v, err := e.Engine.GetCountDistinct(name, bucket, period)
	if err != nil {
		e.Err = err
		return 0
	}
	return v
}

func (e *RepoEvent) IncrementDistinct(name, bucket, val string) {
	e.CounterDistinctIncrements = append(e.CounterDistinctIncrements, CounterDistinctRef{Name: name, Bucket: bucket, Val: val})
}

func (e *RepoEvent) InSet(name, val string) bool {
	v, err := e.Engine.InSet(name, val)
	if err != nil {
		e.Err = err
		return false
	}
	return v
}

func (e *RepoEvent) TakedownAccount() {
	e.AccountTakedown = true
}

func (e *RepoEvent) AddAccountLabel(val string) {
	e.AccountLabels = append(e.AccountLabels, val)
}

func (e *RepoEvent) AddAccountFlag(val string) {
	e.AccountFlags = append(e.AccountFlags, val)
}

func (e *RepoEvent) ReportAccount(reason, comment string) {
	e.AccountReports = append(e.AccountReports, ModReport{ReasonType: reason, Comment: comment})
}

func slackBody(msg string, newLabels, newFlags []string, newReports []ModReport, newTakedown bool) string {
	if len(newLabels) > 0 {
		msg += fmt.Sprintf("New Labels: `%s`\n", strings.Join(newLabels, ", "))
	}
	if len(newFlags) > 0 {
		msg += fmt.Sprintf("New Flags: `%s`\n", strings.Join(newFlags, ", "))
	}
	for _, rep := range newReports {
		msg += fmt.Sprintf("Report `%s`: %s\n", rep.ReasonType, rep.Comment)
	}
	if newTakedown {
		msg += fmt.Sprintf("Takedown!\n")
	}
	return msg
}

// Persists account-level moderation actions: new labels, new flags, new takedowns, and reports.
//
// If necessary, will "purge" identity and account caches, so that state updates will be picked up for subsequent events.
//
// TODO: de-dupe reports based on existing state, similar to other state
func (e *RepoEvent) PersistAccountActions(ctx context.Context) error {

	// de-dupe actions
	newLabels := []string{}
	for _, val := range dedupeStrings(e.AccountLabels) {
		exists := false
		for _, e := range e.Account.AccountNegatedLabels {
			if val == e {
				exists = true
				break
			}
		}
		for _, e := range e.Account.AccountLabels {
			if val == e {
				exists = true
				break
			}
		}
		if !exists {
			newLabels = append(newLabels, val)
		}
	}
	newFlags := []string{}
	for _, val := range dedupeStrings(e.AccountFlags) {
		exists := false
		for _, e := range e.Account.AccountFlags {
			if val == e {
				exists = true
				break
			}
		}
		if !exists {
			newFlags = append(newFlags, val)
		}
	}
	newReports := e.AccountReports
	newTakedown := e.AccountTakedown && !e.Account.Takendown

	if newTakedown || len(newLabels) > 0 || len(newFlags) > 0 || len(newReports) > 0 {
		if e.Engine.SlackWebhookURL != "" {
			msg := fmt.Sprintf("⚠️ Automod Account Action ⚠️\n")
			msg += fmt.Sprintf("`%s` / `%s` / <https://bsky.app/profile/%s|bsky> / <https://admin.prod.bsky.dev/repositories/%s|ozone>\n",
				e.Account.Identity.DID,
				e.Account.Identity.Handle,
				e.Account.Identity.DID,
				e.Account.Identity.DID,
			)
			msg = slackBody(msg, newLabels, newFlags, newReports, newTakedown)
			if err := e.Engine.SendSlackMsg(ctx, msg); err != nil {
				e.Logger.Error("sending slack webhook", "err", err)
			}
		}
	}

	// flags don't require admin auth
	needsPurge := false
	if len(newFlags) > 0 {
		e.Engine.Flags.Add(ctx, e.Account.Identity.DID.String(), newFlags)
		needsPurge = true
	}

	if e.Engine.AdminClient == nil {
		return nil
	}

	xrpcc := e.Engine.AdminClient
	if len(newLabels) > 0 {
		comment := "automod"
		_, err := comatproto.AdminEmitModerationEvent(ctx, xrpcc, &comatproto.AdminEmitModerationEvent_Input{
			CreatedBy: xrpcc.Auth.Did,
			Event: &comatproto.AdminEmitModerationEvent_Input_Event{
				AdminDefs_ModEventLabel: &comatproto.AdminDefs_ModEventLabel{
					CreateLabelVals: newLabels,
					NegateLabelVals: []string{},
					Comment:         &comment,
				},
			},
			Subject: &comatproto.AdminEmitModerationEvent_Input_Subject{
				AdminDefs_RepoRef: &comatproto.AdminDefs_RepoRef{
					Did: e.Account.Identity.DID.String(),
				},
			},
		})
		if err != nil {
			return err
		}
		needsPurge = true
	}
	for _, mr := range newReports {
		_, err := comatproto.ModerationCreateReport(ctx, xrpcc, &comatproto.ModerationCreateReport_Input{
			ReasonType: &mr.ReasonType,
			Reason:     &mr.Comment,
			Subject: &comatproto.ModerationCreateReport_Input_Subject{
				AdminDefs_RepoRef: &comatproto.AdminDefs_RepoRef{
					Did: e.Account.Identity.DID.String(),
				},
			},
		})
		if err != nil {
			return err
		}
	}
	if newTakedown {
		comment := "automod"
		_, err := comatproto.AdminEmitModerationEvent(ctx, xrpcc, &comatproto.AdminEmitModerationEvent_Input{
			CreatedBy: xrpcc.Auth.Did,
			Event: &comatproto.AdminEmitModerationEvent_Input_Event{
				AdminDefs_ModEventTakedown: &comatproto.AdminDefs_ModEventTakedown{
					Comment: &comment,
				},
			},
			Subject: &comatproto.AdminEmitModerationEvent_Input_Subject{
				AdminDefs_RepoRef: &comatproto.AdminDefs_RepoRef{
					Did: e.Account.Identity.DID.String(),
				},
			},
		})
		if err != nil {
			return err
		}
		needsPurge = true
	}
	if needsPurge {
		return e.Engine.PurgeAccountCaches(ctx, e.Account.Identity.DID)
	}
	return nil
}

func (e *RepoEvent) PersistActions(ctx context.Context) error {
	return e.PersistAccountActions(ctx)
}

func (e *RepoEvent) PersistCounters(ctx context.Context) error {
	// TODO: dedupe this array
	for _, ref := range e.CounterIncrements {
		if ref.Period != nil {
			err := e.Engine.Counters.IncrementPeriod(ctx, ref.Name, ref.Val, *ref.Period)
			if err != nil {
				return err
			}
		} else {
			err := e.Engine.Counters.Increment(ctx, ref.Name, ref.Val)
			if err != nil {
				return err
			}
		}
	}
	for _, ref := range e.CounterDistinctIncrements {
		err := e.Engine.Counters.IncrementDistinct(ctx, ref.Name, ref.Bucket, ref.Val)
		if err != nil {
			return err
		}
	}
	return nil
}

func (e *RepoEvent) CanonicalLogLine() {
	e.Logger.Info("canonical-event-line",
		"accountLabels", e.AccountLabels,
		"accountFlags", e.AccountFlags,
		"accountTakedown", e.AccountTakedown,
		"accountReports", len(e.AccountReports),
	)
}

type IdentityEvent struct {
	RepoEvent
}

type RecordEvent struct {
	RepoEvent

	Record         any
	Collection     string
	RecordKey      string
	CID            string
	RecordLabels   []string
	RecordTakedown bool
	RecordReports  []ModReport
	RecordFlags    []string
	// TODO: commit metadata
}

func (e *RecordEvent) TakedownRecord() {
	e.RecordTakedown = true
}

func (e *RecordEvent) AddRecordLabel(val string) {
	e.RecordLabels = append(e.RecordLabels, val)
}

func (e *RecordEvent) AddRecordFlag(val string) {
	e.RecordFlags = append(e.RecordFlags, val)
}

func (e *RecordEvent) ReportRecord(reason, comment string) {
	e.RecordReports = append(e.RecordReports, ModReport{ReasonType: reason, Comment: comment})
}

// Persists some record-level state: labels, takedowns, reports.
//
// NOTE: this method currently does *not* persist record-level flags to any storage, and does not de-dupe most actions, on the assumption that the record is new (from firehose) and has no existing mod state.
func (e *RecordEvent) PersistRecordActions(ctx context.Context) error {

	// TODO: consider de-duping record-level actions? at least for updates and deletes.
	newLabels := dedupeStrings(e.RecordLabels)
	newFlags := dedupeStrings(e.RecordFlags)
	newReports := e.RecordReports
	newTakedown := e.RecordTakedown
	atURI := fmt.Sprintf("at://%s/%s/%s", e.Account.Identity.DID, e.Collection, e.RecordKey)

	if newTakedown || len(newLabels) > 0 || len(newFlags) > 0 || len(newReports) > 0 {
		if e.Engine.SlackWebhookURL != "" {
			msg := fmt.Sprintf("⚠️ Automod Record Action ⚠️\n")
			msg += fmt.Sprintf("`%s` / `%s` / <https://bsky.app/profile/%s|bsky> / <https://admin.prod.bsky.dev/repositories/%s|ozone>\n",
				e.Account.Identity.DID,
				e.Account.Identity.Handle,
				e.Account.Identity.DID,
				e.Account.Identity.DID,
			)
			msg += fmt.Sprintf("`%s`\n", atURI)
			msg = slackBody(msg, newLabels, newFlags, newReports, newTakedown)
			if err := e.Engine.SendSlackMsg(ctx, msg); err != nil {
				e.Logger.Error("sending slack webhook", "err", err)
			}
		}
	}

	// flags don't require admin auth
	if len(newFlags) > 0 {
		e.Engine.Flags.Add(ctx, atURI, newFlags)
	}

	if e.Engine.AdminClient == nil {
		return nil
	}

	strongRef := comatproto.RepoStrongRef{
		Cid: e.CID,
		Uri: atURI,
	}
	xrpcc := e.Engine.AdminClient
	if len(newLabels) > 0 {
		comment := "automod"
		_, err := comatproto.AdminEmitModerationEvent(ctx, xrpcc, &comatproto.AdminEmitModerationEvent_Input{
			CreatedBy: xrpcc.Auth.Did,
			Event: &comatproto.AdminEmitModerationEvent_Input_Event{
				AdminDefs_ModEventLabel: &comatproto.AdminDefs_ModEventLabel{
					CreateLabelVals: newLabels,
					NegateLabelVals: []string{},
					Comment:         &comment,
				},
			},
			Subject: &comatproto.AdminEmitModerationEvent_Input_Subject{
				RepoStrongRef: &strongRef,
			},
		})
		if err != nil {
			return err
		}
	}
	for _, mr := range newReports {
		_, err := comatproto.ModerationCreateReport(ctx, xrpcc, &comatproto.ModerationCreateReport_Input{
			ReasonType: &mr.ReasonType,
			Reason:     &mr.Comment,
			Subject: &comatproto.ModerationCreateReport_Input_Subject{
				RepoStrongRef: &strongRef,
			},
		})
		if err != nil {
			return err
		}
	}
	if newTakedown {
		comment := "automod"
		_, err := comatproto.AdminEmitModerationEvent(ctx, xrpcc, &comatproto.AdminEmitModerationEvent_Input{
			CreatedBy: xrpcc.Auth.Did,
			Event: &comatproto.AdminEmitModerationEvent_Input_Event{
				AdminDefs_ModEventTakedown: &comatproto.AdminDefs_ModEventTakedown{
					Comment: &comment,
				},
			},
			Subject: &comatproto.AdminEmitModerationEvent_Input_Subject{
				RepoStrongRef: &strongRef,
			},
		})
		if err != nil {
			return err
		}
	}
	return nil
}

func (e *RecordEvent) PersistActions(ctx context.Context) error {
	if err := e.PersistAccountActions(ctx); err != nil {
		return err
	}
	return e.PersistRecordActions(ctx)
}

func (e *RecordEvent) CanonicalLogLine() {
	e.Logger.Info("canonical-event-line",
		"accountLabels", e.AccountLabels,
		"accountFlags", e.AccountFlags,
		"accountTakedown", e.AccountTakedown,
		"accountReports", len(e.AccountReports),
		"recordLabels", e.RecordLabels,
		"recordFlags", e.RecordFlags,
		"recordTakedown", e.RecordTakedown,
		"recordReports", len(e.RecordReports),
	)
}

type RecordDeleteEvent struct {
	RepoEvent

	Collection string
	RecordKey  string
}

type IdentityRuleFunc = func(evt *IdentityEvent) error
type RecordRuleFunc = func(evt *RecordEvent) error
type PostRuleFunc = func(evt *RecordEvent, post *appbsky.FeedPost) error
type ProfileRuleFunc = func(evt *RecordEvent, profile *appbsky.ActorProfile) error
type RecordDeleteRuleFunc = func(evt *RecordDeleteEvent) error
