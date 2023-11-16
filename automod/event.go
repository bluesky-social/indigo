package automod

import (
	"context"
	"fmt"
	"log/slog"

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

// base type for events. events are both containers for data about the event itself (similar to an HTTP request type); aggregate results and state (counters, mod actions) to be persisted after all rules are run; and act as an API for additional network reads and operations.
type Event struct {
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

func (e *Event) GetCount(name, val, period string) int {
	v, err := e.Engine.GetCount(name, val, period)
	if err != nil {
		e.Err = err
		return 0
	}
	return v
}

func (e *Event) InSet(name, val string) bool {
	v, err := e.Engine.InSet(name, val)
	if err != nil {
		e.Err = err
		return false
	}
	return v
}

func (e *Event) Increment(name, val string) {
	e.CounterIncrements = append(e.CounterIncrements, CounterRef{Name: name, Val: val})
}

func (e *Event) TakedownAccount() {
	e.AccountTakedown = true
}

func (e *Event) AddAccountLabel(val string) {
	e.AccountLabels = append(e.AccountLabels, val)
}

func (e *Event) AddAccountFlag(val string) {
	e.AccountFlags = append(e.AccountFlags, val)
}

func (e *Event) ReportAccount(reason, comment string) {
	e.AccountReports = append(e.AccountReports, ModReport{ReasonType: reason, Comment: comment})
}

func (e *Event) PersistAccountActions(ctx context.Context) error {
	if e.Engine.AdminClient == nil {
		return nil
	}
	xrpcc := e.Engine.AdminClient
	if len(e.AccountLabels) > 0 {
		_, err := comatproto.AdminTakeModerationAction(ctx, xrpcc, &comatproto.AdminTakeModerationAction_Input{
			Action:          "com.atproto.admin.defs#flag",
			CreateLabelVals: dedupeStrings(e.AccountLabels),
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
	}
	// TODO: AccountFlags
	for _, mr := range e.AccountReports {
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
	if e.AccountTakedown {
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
	}
	return nil
}

func (e *Event) PersistActions(ctx context.Context) error {
	return e.PersistAccountActions(ctx)
}

func (e *Event) PersistCounters(ctx context.Context) error {
	// TODO: dedupe this array
	for _, ref := range e.CounterIncrements {
		err := e.Engine.Counters.Increment(ctx, ref.Name, ref.Val)
		if err != nil {
			return err
		}
	}
	return nil
}

func (e *Event) CanonicalLogLine() {
	e.Logger.Info("canonical-event-line",
		"accountLabels", e.AccountLabels,
		"accountFlags", e.AccountFlags,
		"accountTakedown", e.AccountTakedown,
		"accountReports", len(e.AccountReports),
	)
}

type IdentityEvent struct {
	Event
}

type RecordEvent struct {
	Event

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
	if e.Engine.AdminClient == nil {
		return nil
	}
	strongRef := comatproto.RepoStrongRef{
		Cid: e.CID,
		Uri: fmt.Sprintf("at://%s/%s/%s", e.Account.Identity.DID, e.Collection, e.RecordKey),
	}
	xrpcc := e.Engine.AdminClient
	if len(e.RecordLabels) > 0 {
		// TODO: this does an action, not just create labels; will update after event refactor
		_, err := comatproto.AdminTakeModerationAction(ctx, xrpcc, &comatproto.AdminTakeModerationAction_Input{
			Action:          "com.atproto.admin.defs#flag",
			CreateLabelVals: dedupeStrings(e.RecordLabels),
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
	// TODO: AccountFlags
	for _, mr := range e.RecordReports {
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
	if e.RecordTakedown {
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

type PostEvent struct {
	RecordEvent

	Post *appbsky.FeedPost
	// TODO: post thread context (root, parent)
}

type IdentityRuleFunc = func(evt *IdentityEvent) error
type RecordRuleFunc = func(evt *RecordEvent) error
type PostRuleFunc = func(evt *PostEvent) error
