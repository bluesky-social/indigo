package did

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

var mrResolvedDidsTotal = promauto.NewCounterVec(prometheus.CounterOpts{
	Name: "multiresolver_resolved_dids_total",
	Help: "Total number of DIDs resolved",
}, []string{"resolver"})

var mrResolveDuration = promauto.NewHistogramVec(prometheus.HistogramOpts{
	Name:    "indigo_multiresolver_resolve_duration_seconds",
	Help:    "A histogram of resolve latencies",
	Buckets: prometheus.ExponentialBuckets(0.001, 2, 15),
}, []string{"resolver"})
