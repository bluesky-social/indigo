package bgs

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

var eventsReceivedCounter = promauto.NewCounterVec(prometheus.CounterOpts{
	Name: "events_received_counter",
	Help: "The total number of events received",
}, []string{"pds"})

var repoCommitsReceivedCounter = promauto.NewCounterVec(prometheus.CounterOpts{
	Name: "repo_commits_received_counter",
	Help: "The total number of events received",
}, []string{"pds"})

var rebasesCounter = promauto.NewCounterVec(prometheus.CounterOpts{
	Name: "event_rebases",
	Help: "The total number of rebase events received",
}, []string{"pds"})

var eventsSentCounter = promauto.NewCounterVec(prometheus.CounterOpts{
	Name: "events_sent_counter",
	Help: "The total number of events sent to consumers",
}, []string{"remote_addr", "user_agent"})
