package events

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

var eventsFromStreamCounter = promauto.NewCounterVec(prometheus.CounterOpts{
	Name: "indigo_repo_stream_events_received_total",
	Help: "Total number of events received from the stream",
}, []string{"remote_addr"})

var bytesFromStreamCounter = promauto.NewCounterVec(prometheus.CounterOpts{
	Name: "indigo_repo_stream_bytes_total",
	Help: "Total bytes received from the stream",
}, []string{"remote_addr"})

var eventsEnqueued = promauto.NewCounterVec(prometheus.CounterOpts{
	Name: "indigo_events_enqueued_for_broadcast_total",
	Help: "Total number of events enqueued to broadcast to subscribers",
}, []string{"pool"})

var eventsBroadcast = promauto.NewCounterVec(prometheus.CounterOpts{
	Name: "indigo_events_broadcast_total",
	Help: "Total number of events broadcast to subscribers",
}, []string{"pool"})
