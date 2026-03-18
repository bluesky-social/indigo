package backfill

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

var backfillJobsEnqueued = promauto.NewCounterVec(prometheus.CounterOpts{
	Name: "backfill_jobs_enqueued_total",
	Help: "The total number of backfill jobs enqueued",
}, []string{"backfiller_name"})

var backfillJobsProcessed = promauto.NewCounterVec(prometheus.CounterOpts{
	Name: "backfill_jobs_processed_total",
	Help: "The total number of backfill jobs processed",
}, []string{"backfiller_name"})

var backfillRecordsProcessed = promauto.NewCounterVec(prometheus.CounterOpts{
	Name: "backfill_records_processed_total",
	Help: "The total number of backfill records processed",
}, []string{"backfiller_name"})

var backfillOpsBuffered = promauto.NewGaugeVec(prometheus.GaugeOpts{
	Name: "backfill_ops_buffered",
	Help: "The number of backfill operations buffered",
}, []string{"backfiller_name"})

var backfillBytesProcessed = promauto.NewCounterVec(prometheus.CounterOpts{
	Name: "backfill_bytes_processed_total",
	Help: "The total number of backfill bytes processed",
}, []string{"backfiller_name"})

var backfillDispatchSeconds = promauto.NewHistogramVec(prometheus.HistogramOpts{
	Name:    "backfill_dispatch_seconds",
	Help:    "Time spent in the dispatch loop per job (dequeue + setState + semaphore acquire)",
	Buckets: []float64{0.0001, 0.0005, 0.001, 0.005, 0.01, 0.05, 0.1, 0.5, 1},
}, []string{"backfiller_name", "phase"})

var backfillRateLimitWaitSeconds = promauto.NewHistogramVec(prometheus.HistogramOpts{
	Name:    "backfill_ratelimit_wait_seconds",
	Help:    "Time spent waiting on rate limiters before fetching a repo",
	Buckets: []float64{0.001, 0.005, 0.01, 0.05, 0.1, 0.25, 0.5, 1, 2, 5, 10},
}, []string{"backfiller_name", "limiter"})

var backfillRateLimitWaitsTotal = promauto.NewCounterVec(prometheus.CounterOpts{
	Name: "backfill_ratelimit_waits_total",
	Help: "Total number of rate limiter waits by limiter type and PDS host",
}, []string{"backfiller_name", "limiter", "host"})

var backfillRateLimitWaitSecondsByHost = promauto.NewCounterVec(prometheus.CounterOpts{
	Name: "backfill_ratelimit_wait_seconds_by_host",
	Help: "Total seconds spent waiting on PDS rate limiters, by host",
}, []string{"backfiller_name", "host"})
