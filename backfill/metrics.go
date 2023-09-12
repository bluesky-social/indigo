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
