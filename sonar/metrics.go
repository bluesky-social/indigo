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
}, []string{"action", "socket_url", "record_type"})

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
	Help: "The sequence number of the last event processed",
}, []string{"socket_url"})

var lastEvtProcessedAtGauge = promauto.NewGaugeVec(prometheus.GaugeOpts{
	Name: "sonar_last_evt_processed_at",
	Help: "The timestamp of the last event processed",
}, []string{"socket_url"})

var lastEvtCreatedAtGauge = promauto.NewGaugeVec(prometheus.GaugeOpts{
	Name: "sonar_last_evt_created_at",
	Help: "The timestamp of the last event created",
}, []string{"socket_url"})

var lastRecordCreatedAtGauge = promauto.NewGaugeVec(prometheus.GaugeOpts{
	Name: "sonar_last_record_created_at",
	Help: "The timestamp of the last record processed",
}, []string{"socket_url"})

var lastEvtCreatedRecordCreatedGapGauge = promauto.NewGaugeVec(prometheus.GaugeOpts{
	Name: "sonar_last_evt_created_record_created_gap",
	Help: "The gap between the last event's event timestamp and it's record timestamp",
}, []string{"socket_url"})

var lastEvtCreatedEvtProcessedGapGauge = promauto.NewGaugeVec(prometheus.GaugeOpts{
	Name: "sonar_last_evt_created_evt_processed_gap",
	Help: "The gap between the last event's event timestamp and when it was processed by sonar",
}, []string{"socket_url"})

var lastRecordCreatedEvtProcessedGapGauge = promauto.NewGaugeVec(prometheus.GaugeOpts{
	Name: "sonar_last_record_created_evt_processed_gap",
	Help: "The gap between the last record's record timestamp and when it was processed by sonar",
}, []string{"socket_url"})
