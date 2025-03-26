package bgs

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

var eventsEnqueued = promauto.NewCounterVec(prometheus.CounterOpts{
	Name: "indigo_events_enqueued_for_broadcast_total",
	Help: "Total number of events enqueued to broadcast to subscribers",
}, []string{"pool"})

var eventsBroadcast = promauto.NewCounterVec(prometheus.CounterOpts{
	Name: "indigo_events_broadcast_total",
	Help: "Total number of events broadcast to subscribers",
}, []string{"pool"})
