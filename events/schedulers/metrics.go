package schedulers

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

var WorkItemsAdded = promauto.NewCounterVec(prometheus.CounterOpts{
	Name: "indigo_scheduler_work_items_added_total",
	Help: "Total number of work items added to the consumer pool",
}, []string{"pool", "scheduler_type"})

var WorkItemsProcessed = promauto.NewCounterVec(prometheus.CounterOpts{
	Name: "indigo_scheduler_work_items_processed_total",
	Help: "Total number of work items processed by the consumer pool",
}, []string{"pool", "scheduler_type"})

var WorkItemsActive = promauto.NewCounterVec(prometheus.CounterOpts{
	Name: "indigo_scheduler_work_items_active_total",
	Help: "Total number of work items passed into a worker",
}, []string{"pool", "scheduler_type"})

var WorkersActive = promauto.NewGaugeVec(prometheus.GaugeOpts{
	Name: "indigo_scheduler_workers_active",
	Help: "Number of workers currently active",
}, []string{"pool", "scheduler_type"})

// queueLatencyBuckets spans sub-millisecond (healthy: an idle worker is
// waiting) through five minutes (a deep backup). The Prometheus defaults stop
// at 10s, which is where the interesting part of a scheduler backup starts.
var queueLatencyBuckets = []float64{
	0.0005, 0.001, 0.005, 0.01, 0.05, 0.1, 0.5, 1, 5, 10, 30, 60, 300,
}

// queueDepthBuckets are powers of two so a single pathological repo shows up
// as a long tail rather than being averaged away by the many repos sitting at
// depth 0.
var queueDepthBuckets = []float64{
	0, 1, 2, 4, 8, 16, 32, 64, 128, 256, 512, 1024, 4096, 16384,
}

// QueuedItems is the number of items enqueued but not yet picked up by a
// worker. It counts both items buffered in a repo's queue and the single item
// blocked on the feeder channel, so a backup is visible regardless of which
// side of the handoff it accumulates on.
var QueuedItems = promauto.NewGaugeVec(prometheus.GaugeOpts{
	Name: "indigo_scheduler_queued_items",
	Help: "Number of work items enqueued but not yet picked up by a worker",
}, []string{"pool", "scheduler_type", "event_kind"})

// ItemsInFlight is the number of items currently inside a handler. Read
// alongside QueuedItems it separates "every worker is busy" from "workers are
// idle but the queue is deep" (the latter means per-repo serialization, not
// insufficient concurrency, is the constraint).
var ItemsInFlight = promauto.NewGaugeVec(prometheus.GaugeOpts{
	Name: "indigo_scheduler_items_in_flight",
	Help: "Number of work items currently being processed by a worker",
}, []string{"pool", "scheduler_type", "event_kind"})

// RepoQueueDepth observes, once per enqueued item, how many items were already
// buffered for that repo. This is the "which queue" signal: broad backlog
// across many repos versus one hot repo serialized behind itself.
var RepoQueueDepth = promauto.NewHistogramVec(prometheus.HistogramOpts{
	Name:    "indigo_scheduler_repo_queue_depth",
	Help:    "Depth of the per-repo queue an item was appended to, at enqueue time",
	Buckets: queueDepthBuckets,
}, []string{"pool", "scheduler_type"})

// QueueWaitSeconds measures enqueue to worker pickup. Rising wait with a flat
// queue depth means slow handlers; rising both means arrival outpaces drain.
var QueueWaitSeconds = promauto.NewHistogramVec(prometheus.HistogramOpts{
	Name:    "indigo_scheduler_queue_wait_sec",
	Help:    "Time a work item spent queued before a worker picked it up",
	Buckets: queueLatencyBuckets,
}, []string{"pool", "scheduler_type", "event_kind"})

// FeederBlockSeconds measures time AddWork spent blocked handing off to the
// feeder channel. This is the exact point at which scheduler backpressure
// reaches the caller's read loop, so it is what explains upstream cursor lag.
var FeederBlockSeconds = promauto.NewHistogramVec(prometheus.HistogramOpts{
	Name:    "indigo_scheduler_feeder_block_sec",
	Help:    "Time AddWork blocked waiting to hand an item to a worker",
	Buckets: queueLatencyBuckets,
}, []string{"pool", "scheduler_type", "event_kind"})

// ItemProcessSeconds measures handler duration, split by event kind, which is
// what identifies the kind of work a backed-up queue is backed up with.
var ItemProcessSeconds = promauto.NewHistogramVec(prometheus.HistogramOpts{
	Name:    "indigo_scheduler_item_process_sec",
	Help:    "Duration of the event handler for a work item",
	Buckets: queueLatencyBuckets,
}, []string{"pool", "scheduler_type", "event_kind"})
