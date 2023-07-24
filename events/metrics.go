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

var workItemsInFlight = promauto.NewGaugeVec(prometheus.GaugeOpts{
	Name: "work_items_in_flight",
	Help: "Number of work items currently in flight",
}, []string{"pool"})
