package archiver

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

var eventsReceivedCounter = promauto.NewCounterVec(prometheus.CounterOpts{
	Name: "arc_events_received_counter",
	Help: "The total number of events received",
}, []string{"pds"})

var eventsHandleDuration = promauto.NewHistogramVec(prometheus.HistogramOpts{
	Name:    "arc_events_handle_duration",
	Help:    "A histogram of handleFedEvent latencies",
	Buckets: prometheus.ExponentialBuckets(0.001, 2, 15),
}, []string{"pds"})

var repoCommitsReceivedCounter = promauto.NewCounterVec(prometheus.CounterOpts{
	Name: "arc_repo_commits_received_counter",
	Help: "The total number of events received",
}, []string{"pds"})

var repoCommitsResultCounter = promauto.NewCounterVec(prometheus.CounterOpts{
	Name: "arc_repo_commits_result_counter",
	Help: "The results of commit events received",
}, []string{"pds", "status"})

var externalUserCreationAttempts = promauto.NewCounter(prometheus.CounterOpts{
	Name: "archiver_external_user_creation_attempts",
	Help: "The total number of external users created",
})

var newUsersDiscovered = promauto.NewCounter(prometheus.CounterOpts{
	Name: "archiver_new_users_discovered",
	Help: "The total number of new users discovered directly from the firehose (not from refs)",
})

var userLookupDuration = promauto.NewHistogram(prometheus.HistogramOpts{
	Name:    "arc_user_lookup_duration",
	Help:    "A histogram of user lookup latencies",
	Buckets: prometheus.ExponentialBuckets(0.001, 2, 15),
})

var newUserDiscoveryDuration = promauto.NewHistogram(prometheus.HistogramOpts{
	Name:    "arc_new_user_discovery_duration",
	Help:    "A histogram of new user discovery latencies",
	Buckets: prometheus.ExponentialBuckets(0.001, 2, 15),
})

// From old indexer code

var userCrawlsEnqueued = promauto.NewCounter(prometheus.CounterOpts{
	Name: "arc_user_crawls_enqueued",
	Help: "Number of user crawls enqueued",
})

var reposFetched = promauto.NewCounterVec(prometheus.CounterOpts{
	Name: "arc_repos_fetched",
	Help: "Number of repos fetched",
}, []string{"status"})

var catchupEventsEnqueued = promauto.NewCounterVec(prometheus.CounterOpts{
	Name: "arc_catchup_events_enqueued",
	Help: "Number of catchup events enqueued",
}, []string{"how"})

var catchupEventsFailed = promauto.NewCounterVec(prometheus.CounterOpts{
	Name: "arc_catchup_events_failed",
	Help: "Number of catchup events processed",
}, []string{"err"})

var catchupReposGauge = promauto.NewGauge(prometheus.GaugeOpts{
	Name: "arc_catchup_repos",
	Help: "Number of repos waiting on catchup",
})
