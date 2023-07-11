package sonar

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

// Initialize Prometheus Metrics for total number of posts processed
var eventsProcessedCounter = promauto.NewCounterVec(prometheus.CounterOpts{
	Name: "sonar_events_processed_total",
	Help: "The total number of firehose events processed by Sonar",
}, []string{"event_type", "socket_url"})

var rebasesProcessedCounter = promauto.NewCounterVec(prometheus.CounterOpts{
	Name: "sonar_rebases_processed_total",
	Help: "The total number of rebase operations processed by Sonar",
}, []string{"socket_url"})

var recordsProcessedCounter = promauto.NewCounterVec(prometheus.CounterOpts{
	Name: "sonar_records_processed_total",
	Help: "The total number of records processed by Sonar",
}, []string{"record_type", "socket_url"})

var quoteRepostsProcessedCounter = promauto.NewCounterVec(prometheus.CounterOpts{
	Name: "sonar_quote_reposts_processed_total",
	Help: "The total number quote repost operations processed by Sonar",
}, []string{"socket_url"})

var opsProcessedCounter = promauto.NewCounterVec(prometheus.CounterOpts{
	Name: "sonar_ops_processed_total",
	Help: "The total number of repo operations processed by Sonar",
}, []string{"kind", "op_path", "socket_url"})

// Initialize Prometheus metrics for duration of processing events
var eventProcessingDurationHistogram = promauto.NewHistogramVec(prometheus.HistogramOpts{
	Name:    "sonar_event_processing_duration_seconds",
	Help:    "The amount of time it takes to process a firehose event",
	Buckets: prometheus.ExponentialBuckets(0.001, 2, 15),
}, []string{"socket_url"})

var lastSeqGauge = promauto.NewGaugeVec(prometheus.GaugeOpts{
	Name: "sonar_last_seq",
	Help: "The last sequence number processed",
}, []string{"socket_url"})

var lastSeqProcessedAtGauge = promauto.NewGaugeVec(prometheus.GaugeOpts{
	Name: "sonar_last_seq_processed_at",
	Help: "The timestamp of the last sequence number processed",
}, []string{"socket_url"})

var lastSeqCreatedAtGauge = promauto.NewGaugeVec(prometheus.GaugeOpts{
	Name: "sonar_last_seq_created_at",
	Help: "The timestamp of the last sequence number created",
}, []string{"socket_url"})

var lastSeqCommittedAtGauge = promauto.NewGaugeVec(prometheus.GaugeOpts{
	Name: "sonar_last_seq_committed_at",
	Help: "The commit timestamp of the last sequence number processed",
}, []string{"socket_url"})
