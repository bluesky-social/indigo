package slurper

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

var connectedInbound = promauto.NewGauge(prometheus.GaugeOpts{
	Name: "relay_connected_inbound",
	Help: "Number of inbound firehoses we are consuming",
})
