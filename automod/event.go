package automod

import (
	"context"
	"fmt"
	"log/slog"

	comatproto "github.com/bluesky-social/indigo/api/atproto"
	appbsky "github.com/bluesky-social/indigo/api/bsky"
	"github.com/bluesky-social/indigo/atproto/identity"
)

type ModReport struct {
	ReasonType string
	Comment    string
}

// information about a repo/account/identity, always pre-populated and relevant to many rules
type AccountMeta struct {
	Identity *identity.Identity
	// TODO: createdAt / age
}

// base type for events. events are both containers for data about the event itself (similar to an HTTP request type); aggregate results and state (counters, mod actions) to be persisted after all rules are run; and act as an API for additional network reads and operations.
type Event struct {
	Engine            *Engine
	Err               error
	Logger            *slog.Logger
	Account           AccountMeta
	CounterIncrements []string
	AccountLabels     []string
	AccountFlags      []string
	AccountReports    []ModReport
	AccountTakedown   bool
}

func (e *Event) CountTotal(key string) int {
	v, err := e.Engine.GetCount(key, PeriodTotal)
	if err != nil {
		e.Err = err
		return 0
	}
	return v
}

func (e *Event) CountDay(key string) int {
	v, err := e.Engine.GetCount(key, PeriodDay)
	if err != nil {
		e.Err = err
		return 0
	}
	return v
}

func (e *Event) CountHour(key string) int {
	v, err := e.Engine.GetCount(key, PeriodHour)
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

func (e *Event) IncrementCounter(key string) {
	e.CounterIncrements = append(e.CounterIncrements, key)
}

func (e *Event) TakedownAccount() {
	e.AccountTakedown = true
}

func (e *Event) AddLabelAccount(val string) {
	e.AccountLabels = append(e.AccountLabels, val)
}

func (e *Event) AddFlag(val string) {
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
			Action:          "com.atproto.admin.defs#createLabels",
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

func (e *Event) PersistCounters(ctx context.Context) error {
	for _, k := range dedupeStrings(e.CounterIncrements) {
		err := e.Engine.Counters.Increment(ctx, k)
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

func (e *RecordEvent) Takedown() {
	e.RecordTakedown = true
}

func (e *RecordEvent) AddLabel(val string) {
	e.RecordLabels = append(e.RecordLabels, val)
}

func (e *RecordEvent) AddFlag(val string) {
	e.RecordFlags = append(e.RecordFlags, val)
}

func (e *RecordEvent) Report(reason, comment string) {
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
		_, err := comatproto.AdminTakeModerationAction(ctx, xrpcc, &comatproto.AdminTakeModerationAction_Input{
			Action:          "com.atproto.admin.defs#createLabels",
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
