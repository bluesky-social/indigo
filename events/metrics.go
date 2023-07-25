package events

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

var eventsFromStreamCounter = promauto.NewCounterVec(prometheus.CounterOpts{
	Name: "repo_stream_events_received_total",
	Help: "Total number of events received from the stream",
}, []string{"remote_addr"})

var bytesFromStreamCounter = promauto.NewCounterVec(prometheus.CounterOpts{
	Name: "repo_stream_bytes_total",
	Help: "Total bytes received from the stream",
}, []string{"remote_addr"})

var workItemsAdded = promauto.NewCounterVec(prometheus.CounterOpts{
	Name: "work_items_added_total",
	Help: "Total number of work items added to the consumer pool",
}, []string{"pool"})

var workItemsProcessed = promauto.NewCounterVec(prometheus.CounterOpts{
	Name: "work_items_processed_total",
	Help: "Total number of work items processed by the consumer pool",
}, []string{"pool"})

var workItemsActive = promauto.NewCounterVec(prometheus.CounterOpts{
	Name: "work_items_active_total",
	Help: "Total number of work items passed into a worker",
}, []string{"pool"})

var eventsEnqueued = promauto.NewCounterVec(prometheus.CounterOpts{
	Name: "events_enqueued_for_broadcast_total",
	Help: "Total number of events enqueued to broadcast to subscribers",
}, []string{"pool"})

var eventsBroadcast = promauto.NewCounterVec(prometheus.CounterOpts{
	Name: "events_broadcast_total",
	Help: "Total number of events broadcast to subscribers",
}, []string{"pool"})
