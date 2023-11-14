package automod

import (
	appbsky "github.com/bluesky-social/indigo/api/bsky"
	"github.com/bluesky-social/indigo/atproto/identity"
)

type ModReport struct {
	Reason  string
	Comment string
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
	e.AccountReports = append(e.AccountReports, ModReport{Reason: reason, Comment: comment})
}

type IdentityEvent struct {
	Event
}

type RecordEvent struct {
	Event

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
	e.RecordReports = append(e.RecordReports, ModReport{Reason: reason, Comment: comment})
}

type PostEvent struct {
	RecordEvent

	Post *appbsky.FeedPost
	// TODO: post thread context (root, parent)
}

type IdentityRuleFunc = func(evt *IdentityEvent) error
type RecordRuleFunc = func(evt *RecordEvent) error
type PostRuleFunc = func(evt *PostEvent) error
