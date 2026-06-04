package jetstream

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

var (
	jetstreamEventsFromStreamCounter = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "indigo_repo_jetstream_events_received_total",
		Help: "Total number of events received from the stream",
	}, []string{"remote_addr"})
)
