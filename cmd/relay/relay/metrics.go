package relay

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

// TODO: expose an accessor instead of exporting
var EventsReceivedCounter = promauto.NewCounterVec(prometheus.CounterOpts{
	Name: "events_received_counter",
	Help: "The total number of events received",
}, []string{"pds"})

var eventsWarningsCounter = promauto.NewCounterVec(prometheus.CounterOpts{
	Name: "events_warn_counter",
	Help: "Events received with warnings",
}, []string{"pds", "warn"})

var eventsHandleDuration = promauto.NewHistogramVec(prometheus.HistogramOpts{
	Name:    "events_handle_duration",
	Help:    "A histogram of handleFedEvent latencies",
	Buckets: prometheus.ExponentialBuckets(0.001, 2, 15),
}, []string{"pds"})

var repoCommitsReceivedCounter = promauto.NewCounterVec(prometheus.CounterOpts{
	Name: "repo_commits_received_counter",
	Help: "The total number of commit events received",
}, []string{"pds"})
var repoSyncReceivedCounter = promauto.NewCounterVec(prometheus.CounterOpts{
	Name: "repo_sync_received_counter",
	Help: "The total number of sync events received",
}, []string{"pds"})

var eventsSentCounter = promauto.NewCounterVec(prometheus.CounterOpts{
	Name: "events_sent_counter",
	Help: "The total number of events sent to consumers",
}, []string{"remote_addr", "user_agent"})

var externalUserCreationAttempts = promauto.NewCounter(prometheus.CounterOpts{
	Name: "relay_external_user_creation_attempts",
	Help: "The total number of external users created",
})

var newUsersDiscovered = promauto.NewCounter(prometheus.CounterOpts{
	Name: "relay_new_users_discovered",
	Help: "The total number of new users discovered directly from the firehose (not from refs)",
})

var newUserDiscoveryDuration = promauto.NewHistogram(prometheus.HistogramOpts{
	Name:    "relay_new_user_discovery_duration",
	Help:    "A histogram of new user discovery latencies",
	Buckets: prometheus.ExponentialBuckets(0.001, 2, 15),
})

var accountVerifyWarnings = promauto.NewCounterVec(prometheus.CounterOpts{
	Name: "validator_account_verify_warnings",
	Help: "things that have been a little bit wrong with account messages",
}, []string{"host", "warn"})
