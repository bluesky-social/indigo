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
	Name string
	Val  string
}

// base type for events specific to an account, usually derived from a repo event stream message (one such message may result in multiple `RepoEvent`)
//
// events are both containers for data about the event itself (similar to an HTTP request type); aggregate results and state (counters, mod actions) to be persisted after all rules are run; and act as an API for additional network reads and operations.
type RepoEvent struct {
	Engine            *Engine
	Err               error
	Logger            *slog.Logger
	Account           AccountMeta
	CounterIncrements []CounterRef
	AccountLabels     []string
	AccountFlags      []string
	AccountReports    []ModReport
	AccountTakedown   bool
}

func (e *RepoEvent) GetCount(name, val, period string) int {
	v, err := e.Engine.GetCount(name, val, period)
	if err != nil {
		e.Err = err
		return 0
	}
	return v
}

func (e *RepoEvent) InSet(name, val string) bool {
	v, err := e.Engine.InSet(name, val)
	if err != nil {
		e.Err = err
		return false
	}
	return v
}

func (e *RepoEvent) Increment(name, val string) {
	e.CounterIncrements = append(e.CounterIncrements, CounterRef{Name: name, Val: val})
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

func (e *RepoEvent) PersistAccountActions(ctx context.Context) error {

	// de-dupe actions
	newLabels := []string{}
	for _, val := range dedupeStrings(e.AccountLabels) {
		exists := false
		for _, e := range e.Account.AccountNegLabels {
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
	// TODO: persist and de-dupe flags? in mod service?
	newFlags := dedupeStrings(e.AccountFlags)
	// TODO: de-dupe reports based on history?
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

	if e.Engine.AdminClient == nil {
		return nil
	}

	needsPurge := false
	xrpcc := e.Engine.AdminClient
	if len(newLabels) > 0 {
		_, err := comatproto.AdminTakeModerationAction(ctx, xrpcc, &comatproto.AdminTakeModerationAction_Input{
			Action:          "com.atproto.admin.defs#flag",
			CreateLabelVals: newLabels,
			Reason:          "automod",
			CreatedBy:       xrpcc.Auth.Did,
			Subject: &comatproto.AdminTakeModerationAction_Input_Subject{
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
	// TODO: AccountFlags
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
		_, err := comatproto.AdminTakeModerationAction(ctx, xrpcc, &comatproto.AdminTakeModerationAction_Input{
			Action:    "com.atproto.admin.defs#takedown",
			Reason:    "automod",
			CreatedBy: xrpcc.Auth.Did,
			Subject: &comatproto.AdminTakeModerationAction_Input_Subject{
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
		err := e.Engine.Counters.Increment(ctx, ref.Name, ref.Val)
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

func (e *RecordEvent) PersistRecordActions(ctx context.Context) error {

	// TODO: de-dupe actions
	newLabels := dedupeStrings(e.RecordLabels)
	newFlags := dedupeStrings(e.RecordFlags)
	newReports := e.RecordReports
	newTakedown := e.RecordTakedown

	if newTakedown || len(newLabels) > 0 || len(newFlags) > 0 || len(newReports) > 0 {
		if e.Engine.SlackWebhookURL != "" {
			msg := fmt.Sprintf("⚠️ Automod Record Action ⚠️\n")
			msg += fmt.Sprintf("`%s` / `%s` / <https://bsky.app/profile/%s|bsky> / <https://admin.prod.bsky.dev/repositories/%s|ozone>\n",
				e.Account.Identity.DID,
				e.Account.Identity.Handle,
				e.Account.Identity.DID,
				e.Account.Identity.DID,
			)
			msg += fmt.Sprintf("`at://%s/%s/%s`\n", e.Account.Identity.DID, e.Collection, e.RecordKey)
			msg = slackBody(msg, newLabels, newFlags, newReports, newTakedown)
			if err := e.Engine.SendSlackMsg(ctx, msg); err != nil {
				e.Logger.Error("sending slack webhook", "err", err)
			}
		}
	}
	if e.Engine.AdminClient == nil {
		return nil
	}
	strongRef := comatproto.RepoStrongRef{
		Cid: e.CID,
		Uri: fmt.Sprintf("at://%s/%s/%s", e.Account.Identity.DID, e.Collection, e.RecordKey),
	}
	xrpcc := e.Engine.AdminClient
	if len(newLabels) > 0 {
		// TODO: this does an action, not just create labels; will update after event refactor
		_, err := comatproto.AdminTakeModerationAction(ctx, xrpcc, &comatproto.AdminTakeModerationAction_Input{
			Action:          "com.atproto.admin.defs#flag",
			CreateLabelVals: newLabels,
			Reason:          "automod",
			CreatedBy:       xrpcc.Auth.Did,
			Subject: &comatproto.AdminTakeModerationAction_Input_Subject{
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
		_, err := comatproto.AdminTakeModerationAction(ctx, xrpcc, &comatproto.AdminTakeModerationAction_Input{
			Action:    "com.atproto.admin.defs#takedown",
			Reason:    "automod",
			CreatedBy: xrpcc.Auth.Did,
			Subject: &comatproto.AdminTakeModerationAction_Input_Subject{
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

type IdentityRuleFunc = func(evt *IdentityEvent) error
type RecordRuleFunc = func(evt *RecordEvent) error
type PostRuleFunc = func(evt *RecordEvent, post *appbsky.FeedPost) error
type ProfileRuleFunc = func(evt *RecordEvent, profile *appbsky.ActorProfile) error
