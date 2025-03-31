package validator

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

var commitVerifyStarts = promauto.NewCounter(prometheus.CounterOpts{
	Name: "validator_commit_verify_starts",
})

var commitVerifyWarnings = promauto.NewCounterVec(prometheus.CounterOpts{
	Name: "validator_commit_verify_warnings",
}, []string{"host", "warn"})

// verify error and short code for why
var commitVerifyErrors = promauto.NewCounterVec(prometheus.CounterOpts{
	Name: "validator_commit_verify_errors",
}, []string{"host", "err"})

// ok and *fully verified*
var commitVerifyOk = promauto.NewCounterVec(prometheus.CounterOpts{
	Name: "validator_commit_verify_ok",
}, []string{"host"})

// it's ok, but... {old protocol, no previous root cid, ...}
var commitVerifyOkish = promauto.NewCounterVec(prometheus.CounterOpts{
	Name: "validator_commit_verify_okish",
}, []string{"host", "but"})

// verify error and short code for why
var syncVerifyErrors = promauto.NewCounterVec(prometheus.CounterOpts{
	Name: "validator_sync_verify_errors",
}, []string{"host", "err"})
