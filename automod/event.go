package automod

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"time"

	comatproto "github.com/bluesky-social/indigo/api/atproto"
	appbsky "github.com/bluesky-social/indigo/api/bsky"
	"github.com/bluesky-social/indigo/atproto/syntax"
	"github.com/bluesky-social/indigo/xrpc"
)

var (
	// time period within which automod will not re-report an account for the same reasonType
	ReportDupePeriod = 7 * 24 * time.Hour
	// number of reports automod can file per day, for all subjects and types combined (circuit breaker)
	QuotaModReportDay = 50
	// number of takedowns automod can action per day, for all subjects combined (circuit breaker)
	QuotaModTakedownDay = 10
)

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

// Base type for events specific to an account, usually derived from a repo event stream message (one such message may result in multiple `RepoEvent`)
//
// Events are both containers for data about the event itself (similar to an HTTP request type); aggregate results and state (counters, mod actions) to be persisted after all rules are run; and act as an API for additional network reads and operations.
//
// Handling of moderation actions (such as labels, flags, and reports) are deferred until the end of all rule execution, then de-duplicated against any pre-existing actions on the account.
type RepoEvent struct {
	// Back-reference to Engine that is processing this event. Pointer, but must not be nil.
	Engine *Engine
	// Any error encountered while processing the event can be stashed in this field and handled at the end of all processing.
	Err error
	// slog logger handle, with event-specific structured fields pre-populated. Pointer, but expected to not be nil.
	Logger *slog.Logger
	// Metadata for the account (identity) associated with this event (aka, the repo owner)
	Account AccountMeta
	// List of counters which should be incremented as part of processing this event. These are collected during rule execution and persisted in bulk at the end.
	CounterIncrements []CounterRef
	// Similar to "CounterIncrements", but for "distinct" style counters
	CounterDistinctIncrements []CounterDistinctRef // TODO: better variable names
	// Label values which should be applied to the overall account, as a result of rule execution.
	AccountLabels []string
	// Moderation flags (similar to labels, but private) which should be applied to the overall account, as a result of rule execution.
	AccountFlags []string
	// Reports which should be filed against this account, as a result of rule execution.
	AccountReports []ModReport
	// If "true", indicates that a rule indicates that the entire account should have a takedown.
	AccountTakedown bool
}

// Immediate fetches a count from the event's engine's countstore. Returns 0 by default (if counter has never been incremented).
//
// "name" is the counter namespace.
// "val" is the specific counter with that namespace.
// "period" is the time period bucke (one of the fixed "Period*" values)
func (e *RepoEvent) GetCount(name, val, period string) int {
	v, err := e.Engine.GetCount(name, val, period)
	if err != nil {
		e.Err = err
		return 0
	}
	return v
}

// Enqueues the named counter to be incremented at the end of all rule processing. Will automatically increment for all time periods.
//
// "name" is the counter namespace.
// "val" is the specific counter with that namespace.
func (e *RepoEvent) Increment(name, val string) {
	e.CounterIncrements = append(e.CounterIncrements, CounterRef{Name: name, Val: val})
}

// Enqueues the named counter to be incremented at the end of all rule processing. Will only increment the indicated time period bucket.
func (e *RepoEvent) IncrementPeriod(name, val string, period string) {
	e.CounterIncrements = append(e.CounterIncrements, CounterRef{Name: name, Val: val, Period: &period})
}

// Immediate fetches an estimated (statistical) count of distinct string values in the indicated bucket and time period.
func (e *RepoEvent) GetCountDistinct(name, bucket, period string) int {
	v, err := e.Engine.GetCountDistinct(name, bucket, period)
	if err != nil {
		e.Err = err
		return 0
	}
	return v
}

// Enqueues the named "distinct value" counter based on the supplied string value ("val") to be incremented at the end of all rule processing. Will automatically increment for all time periods.
func (e *RepoEvent) IncrementDistinct(name, bucket, val string) {
	e.CounterDistinctIncrements = append(e.CounterDistinctIncrements, CounterDistinctRef{Name: name, Bucket: bucket, Val: val})
}

// Checks the Engine's setstore for whether the indicated "val" is a member of the "name" set.
func (e *RepoEvent) InSet(name, val string) bool {
	v, err := e.Engine.InSet(name, val)
	if err != nil {
		e.Err = err
		return false
	}
	return v
}

// Enqueues the entire account to be taken down at the end of rule processing.
func (e *RepoEvent) TakedownAccount() {
	e.AccountTakedown = true
}

// Enqueues the provided label (string value) to be added to the account at the end of rule processing.
func (e *RepoEvent) AddAccountLabel(val string) {
	e.AccountLabels = append(e.AccountLabels, val)
}

// Enqueues the provided flag (string value) to be recorded (in the Engine's flagstore) at the end of rule processing.
func (e *RepoEvent) AddAccountFlag(val string) {
	e.AccountFlags = append(e.AccountFlags, val)
}

// Enqueues a moderation report to be filed against the account at the end of rule processing.
func (e *RepoEvent) ReportAccount(reason, comment string) {
	if comment == "" {
		comment = "(no comment)"
	}
	comment = "automod: " + comment
	e.AccountReports = append(e.AccountReports, ModReport{ReasonType: reason, Comment: comment})
}

func slackBody(header string, acct AccountMeta, newLabels, newFlags []string, newReports []ModReport, newTakedown bool) string {
	msg := header
	msg += fmt.Sprintf("`%s` / `%s` / <https://bsky.app/profile/%s|bsky> / <https://admin.prod.bsky.dev/repositories/%s|ozone>\n",
		acct.Identity.DID,
		acct.Identity.Handle,
		acct.Identity.DID,
		acct.Identity.DID,
	)
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

func dedupeLabelActions(labels, existing, existingNegated []string) []string {
	newLabels := []string{}
	for _, val := range dedupeStrings(labels) {
		exists := false
		for _, e := range existingNegated {
			if val == e {
				exists = true
				break
			}
		}
		for _, e := range existing {
			if val == e {
				exists = true
				break
			}
		}
		if !exists {
			newLabels = append(newLabels, val)
		}
	}
	return newLabels
}

func dedupeFlagActions(flags, existing []string) []string {
	newFlags := []string{}
	for _, val := range dedupeStrings(flags) {
		exists := false
		for _, e := range existing {
			if val == e {
				exists = true
				break
			}
		}
		if !exists {
			newFlags = append(newFlags, val)
		}
	}
	return newFlags
}

func dedupeReportActions(evt *RepoEvent, reports []ModReport) []ModReport {
	newReports := []ModReport{}
	for _, r := range reports {
		counterName := "automod-account-report-" + reasonShortName(r.ReasonType)
		existing := evt.GetCount(counterName, evt.Account.Identity.DID.String(), PeriodDay)
		if existing > 0 {
			evt.Logger.Debug("skipping account report due to counter", "existing", existing, "reason", reasonShortName(r.ReasonType))
		} else {
			evt.Increment(counterName, evt.Account.Identity.DID.String())
			newReports = append(newReports, r)
		}
	}
	return newReports
}

func circuitBreakReports(evt *RepoEvent, reports []ModReport) []ModReport {
	if len(reports) == 0 {
		return []ModReport{}
	}
	if evt.GetCount("automod-quota", "report", PeriodDay) >= QuotaModReportDay {
		evt.Logger.Warn("CIRCUIT BREAKER: automod reports")
		return []ModReport{}
	}
	evt.Increment("automod-quota", "report")
	return reports
}

func circuitBreakTakedown(evt *RepoEvent, takedown bool) bool {
	if !takedown {
		return takedown
	}
	if evt.GetCount("automod-quota", "takedown", PeriodDay) >= QuotaModTakedownDay {
		evt.Logger.Warn("CIRCUIT BREAKER: automod takedowns")
		return false
	}
	evt.Increment("automod-quota", "takedown")
	return takedown
}

// Creates a moderation report, but checks first if there was a similar recent one, and skips if so.
//
// Returns a bool indicating if a new report was created.
func createReportIfFresh(ctx context.Context, xrpcc *xrpc.Client, evt RepoEvent, mr ModReport) (bool, error) {
	// before creating a report, query to see if automod has already reported this account in the past week for the same reason
	// NOTE: this is running in an inner loop (if there are multiple reports), which is a bit inefficient, but seems acceptable

	// AdminQueryModerationEvents(ctx context.Context, c *xrpc.Client, createdBy string, cursor string, inc ludeAllUserRecords bool, limit int64, sortDirection string, subject string, types []string)
	resp, err := comatproto.AdminQueryModerationEvents(ctx, xrpcc, xrpcc.Auth.Did, "", false, 5, "", evt.Account.Identity.DID.String(), []string{"com.atproto.admin.defs#modEventReport"})
	if err != nil {
		return false, err
	}
	for _, modEvt := range resp.Events {
		// defensively ensure that our query params worked correctly
		if modEvt.Event.AdminDefs_ModEventReport == nil || modEvt.CreatedBy != xrpcc.Auth.Did || modEvt.Subject.AdminDefs_RepoRef == nil || modEvt.Subject.AdminDefs_RepoRef.Did != evt.Account.Identity.DID.String() || (modEvt.Event.AdminDefs_ModEventReport.ReportType != nil && *modEvt.Event.AdminDefs_ModEventReport.ReportType != mr.ReasonType) {
			continue
		}
		// igonre if older
		created, err := syntax.ParseDatetime(modEvt.CreatedAt)
		if err != nil {
			return false, err
		}
		if time.Since(created.Time()) > ReportDupePeriod {
			continue
		}

		// there is a recent report which is similar to this one
		evt.Logger.Info("skipping duplicate account report due to API check")
		return false, nil
	}

	evt.Logger.Info("reporting account", "reasonType", mr.ReasonType, "comment", mr.Comment)
	_, err = comatproto.ModerationCreateReport(ctx, xrpcc, &comatproto.ModerationCreateReport_Input{
		ReasonType: &mr.ReasonType,
		Reason:     &mr.Comment,
		Subject: &comatproto.ModerationCreateReport_Input_Subject{
			AdminDefs_RepoRef: &comatproto.AdminDefs_RepoRef{
				Did: evt.Account.Identity.DID.String(),
			},
		},
	})
	return true, nil
}

// Persists account-level moderation actions: new labels, new flags, new takedowns, and reports.
//
// If necessary, will "purge" identity and account caches, so that state updates will be picked up for subsequent events.
//
// Note that this method expects to run *before* counts are persisted (it accesses and updates some counts)
func (e *RepoEvent) PersistAccountActions(ctx context.Context) error {

	// de-dupe actions
	newLabels := dedupeLabelActions(e.AccountLabels, e.Account.AccountLabels, e.Account.AccountNegatedLabels)
	newFlags := dedupeFlagActions(e.AccountFlags, e.Account.AccountFlags)

	// don't report the same account multiple times on the same day for the same reason. this is a quick check; we also query the mod service API just before creating the report.
	newReports := circuitBreakReports(e, dedupeReportActions(e, e.AccountReports))
	newTakedown := circuitBreakTakedown(e, e.AccountTakedown && !e.Account.Takendown)

	anyModActions := newTakedown || len(newLabels) > 0 || len(newFlags) > 0 || len(newReports) > 0
	if anyModActions && e.Engine.SlackWebhookURL != "" {
		msg := slackBody("⚠️ Automod Account Action ⚠️\n", e.Account, newLabels, newFlags, newReports, newTakedown)
		if err := e.Engine.SendSlackMsg(ctx, msg); err != nil {
			e.Logger.Error("sending slack webhook", "err", err)
		}
	}

	// flags don't require admin auth
	if len(newFlags) > 0 {
		e.Engine.Flags.Add(ctx, e.Account.Identity.DID.String(), newFlags)
	}

	// if we can't actually talk to service, bail out early
	if e.Engine.AdminClient == nil {
		return nil
	}

	xrpcc := e.Engine.AdminClient

	if len(newLabels) > 0 {
		e.Logger.Info("labeling record", "newLabels", newLabels)
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
	}

	// reports are additionally de-duped when persisting the action, so track with a flag
	createdReports := false
	for _, mr := range newReports {
		created, err := createReportIfFresh(ctx, xrpcc, *e, mr)
		if err != nil {
			return err
		}
		if created {
			createdReports = true
		}
	}

	if newTakedown {
		e.Logger.Warn("account-takedown")
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
	}

	needCachePurge := newTakedown || len(newLabels) > 0 || len(newFlags) > 0 || createdReports
	if needCachePurge {
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

// Alias of RepoEvent
type IdentityEvent struct {
	RepoEvent
}

// Extends RepoEvent. Represents the creation of a single record in the given repository.
type RecordEvent struct {
	RepoEvent

	// The un-marshalled record, as a go struct, from the api/atproto or api/bsky type packages.
	Record any
	// The "collection" part of the repo path for this record. Must be an NSID, though this isn't indicated by the type of this field.
	Collection string
	// The "record key" (rkey) part of repo path.
	RecordKey string
	// CID of the canonical CBOR version of the record, as matches the repo value.
	CID string
	// Same as "AccountLabels", but at record-level
	RecordLabels []string
	// Same as "AccountTakedown", but at record-level
	RecordTakedown bool
	// Same as "AccountReports", but at record-level
	RecordReports []ModReport
	// Same as "AccountFlags", but at record-level
	RecordFlags []string
	// TODO: commit metadata
}

// Enqueues the record to be taken down at the end of rule processing.
func (e *RecordEvent) TakedownRecord() {
	e.RecordTakedown = true
}

// Enqueues the provided label (string value) to be added to the record at the end of rule processing.
func (e *RecordEvent) AddRecordLabel(val string) {
	e.RecordLabels = append(e.RecordLabels, val)
}

// Enqueues the provided flag (string value) to be recorded (in the Engine's flagstore) at the end of rule processing.
func (e *RecordEvent) AddRecordFlag(val string) {
	e.RecordFlags = append(e.RecordFlags, val)
}

// Enqueues a moderation report to be filed against the record at the end of rule processing.
func (e *RecordEvent) ReportRecord(reason, comment string) {
	if comment == "" {
		comment = "(automod)"
	} else {
		comment = "automod: " + comment
	}
	e.RecordReports = append(e.RecordReports, ModReport{ReasonType: reason, Comment: comment})
}

// Persists some record-level state: labels, takedowns, reports.
//
// NOTE: this method currently does *not* persist record-level flags to any storage, and does not de-dupe most actions, on the assumption that the record is new (from firehose) and has no existing mod state.
func (e *RecordEvent) PersistRecordActions(ctx context.Context) error {

	// NOTE: record-level actions are *not* currently de-duplicated (aka, the same record could be labeled multiple times, or re-reported, etc)
	newLabels := dedupeStrings(e.RecordLabels)
	newFlags := dedupeStrings(e.RecordFlags)
	newReports := circuitBreakReports(&e.RepoEvent, e.RecordReports)
	newTakedown := circuitBreakTakedown(&e.RepoEvent, e.RecordTakedown)
	atURI := fmt.Sprintf("at://%s/%s/%s", e.Account.Identity.DID, e.Collection, e.RecordKey)

	if newTakedown || len(newLabels) > 0 || len(newFlags) > 0 || len(newReports) > 0 {
		if e.Engine.SlackWebhookURL != "" {
			msg := slackBody("⚠️ Automod Record Action ⚠️\n", e.Account, newLabels, newFlags, newReports, newTakedown)
			msg += fmt.Sprintf("`%s`\n", atURI)
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
		e.Logger.Info("labeling record", "newLabels", newLabels)
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
		e.Logger.Info("reporting record", "reasonType", mr.ReasonType, "comment", mr.Comment)
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
		e.Logger.Warn("record-takedown")
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

// Extends RepoEvent. Represents the deletion of a single record in the given repository.
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
