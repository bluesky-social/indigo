package engine

import (
	"time"
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

// Mutable container for all the possible side-effects from rule execution.
//
// This single type tracks generic effects (eg, counter increments), account-level actions, and record-level actions (even for processing of account-level events which have no possible record-level effects).
type Effects struct {
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
	// Same as "AccountLabels", but at record-level
	RecordLabels []string
	// Same as "AccountFlags", but at record-level
	RecordFlags []string
	// Same as "AccountReports", but at record-level
	RecordReports []ModReport
	// Same as "AccountTakedown", but at record-level
	RecordTakedown bool
}

// Enqueues the named counter to be incremented at the end of all rule processing. Will automatically increment for all time periods.
//
// "name" is the counter namespace.
// "val" is the specific counter with that namespace.
func (e *Effects) Increment(name, val string) {
	e.CounterIncrements = append(e.CounterIncrements, CounterRef{Name: name, Val: val})
}

// Enqueues the named counter to be incremented at the end of all rule processing. Will only increment the indicated time period bucket.
func (e *Effects) IncrementPeriod(name, val string, period string) {
	e.CounterIncrements = append(e.CounterIncrements, CounterRef{Name: name, Val: val, Period: &period})
}

// Enqueues the named "distinct value" counter based on the supplied string value ("val") to be incremented at the end of all rule processing. Will automatically increment for all time periods.
func (e *Effects) IncrementDistinct(name, bucket, val string) {
	e.CounterDistinctIncrements = append(e.CounterDistinctIncrements, CounterDistinctRef{Name: name, Bucket: bucket, Val: val})
}

// Enqueues the provided label (string value) to be added to the account at the end of rule processing.
func (e *Effects) AddAccountLabel(val string) {
	e.AccountLabels = append(e.AccountLabels, val)
}

// Enqueues the provided flag (string value) to be recorded (in the Engine's flagstore) at the end of rule processing.
func (e *Effects) AddAccountFlag(val string) {
	e.AccountFlags = append(e.AccountFlags, val)
}

// Enqueues a moderation report to be filed against the account at the end of rule processing.
func (e *Effects) ReportAccount(reason, comment string) {
	if comment == "" {
		comment = "(no comment)"
	}
	comment = "automod: " + comment
	e.AccountReports = append(e.AccountReports, ModReport{ReasonType: reason, Comment: comment})
}

// Enqueues the entire account to be taken down at the end of rule processing.
func (e *Effects) TakedownAccount() {
	e.AccountTakedown = true
}

// Enqueues the provided label (string value) to be added to the record at the end of rule processing.
func (e *Effects) AddRecordLabel(val string) {
	e.RecordLabels = append(e.RecordLabels, val)
}

// Enqueues the provided flag (string value) to be recorded (in the Engine's flagstore) at the end of rule processing.
func (e *Effects) AddRecordFlag(val string) {
	e.RecordFlags = append(e.RecordFlags, val)
}

// Enqueues a moderation report to be filed against the record at the end of rule processing.
func (e *Effects) ReportRecord(reason, comment string) {
	if comment == "" {
		comment = "(automod)"
	} else {
		comment = "automod: " + comment
	}
	e.RecordReports = append(e.RecordReports, ModReport{ReasonType: reason, Comment: comment})
}

// Enqueues the record to be taken down at the end of rule processing.
func (e *Effects) TakedownRecord() {
	e.RecordTakedown = true
}
