package splitter

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

var eventsSentCounter = promauto.NewCounterVec(prometheus.CounterOpts{
	Name: "spl_events_sent_counter",
	Help: "The total number of events sent to consumers",
}, []string{"remote_addr", "user_agent"})

var activeClientGauge = promauto.NewGauge(prometheus.GaugeOpts{
	Name: "spl_active_clients",
	Help: "Current number of active clients",
})
