package bgs

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

var eventsReceivedCounter = promauto.NewCounterVec(prometheus.CounterOpts{
	Name: "events_received_counter",
	Help: "The total number of events received",
}, []string{"pds"})

var eventsHandleDuration = promauto.NewHistogramVec(prometheus.HistogramOpts{
	Name:    "events_handle_duration",
	Help:    "A histogram of handleFedEvent latencies",
	Buckets: prometheus.ExponentialBuckets(0.001, 2, 15),
}, []string{"pds"})

var repoCommitsReceivedCounter = promauto.NewCounterVec(prometheus.CounterOpts{
	Name: "repo_commits_received_counter",
	Help: "The total number of events received",
}, []string{"pds"})

var repoCommitsResultCounter = promauto.NewCounterVec(prometheus.CounterOpts{
	Name: "repo_commits_result_counter",
	Help: "The results of commit events received",
}, []string{"pds", "status"})

var rebasesCounter = promauto.NewCounterVec(prometheus.CounterOpts{
	Name: "event_rebases",
	Help: "The total number of rebase events received",
}, []string{"pds"})

var eventsSentCounter = promauto.NewCounterVec(prometheus.CounterOpts{
	Name: "events_sent_counter",
	Help: "The total number of events sent to consumers",
}, []string{"remote_addr", "user_agent"})

var externalUserCreationAttempts = promauto.NewCounter(prometheus.CounterOpts{
	Name: "bgs_external_user_creation_attempts",
	Help: "The total number of external users created",
})

var connectedInbound = promauto.NewGauge(prometheus.GaugeOpts{
	Name: "bgs_connected_inbound",
	Help: "Number of inbound firehoses we are consuming",
})

var compactionDuration = promauto.NewHistogram(prometheus.HistogramOpts{
	Name:    "compaction_duration",
	Help:    "A histogram of compaction latencies",
	Buckets: prometheus.ExponentialBuckets(0.001, 3, 14),
})

var compactionQueueDepth = promauto.NewGauge(prometheus.GaugeOpts{
	Name: "compaction_queue_depth",
	Help: "The current depth of the compaction queue",
})

var newUsersDiscovered = promauto.NewCounter(prometheus.CounterOpts{
	Name: "bgs_new_users_discovered",
	Help: "The total number of new users discovered directly from the firehose (not from refs)",
})

var userLookupDuration = promauto.NewHistogram(prometheus.HistogramOpts{
	Name:    "relay_user_lookup_duration",
	Help:    "A histogram of user lookup latencies",
	Buckets: prometheus.ExponentialBuckets(0.001, 2, 15),
})

var newUserDiscoveryDuration = promauto.NewHistogram(prometheus.HistogramOpts{
	Name:    "relay_new_user_discovery_duration",
	Help:    "A histogram of new user discovery latencies",
	Buckets: prometheus.ExponentialBuckets(0.001, 2, 15),
})
