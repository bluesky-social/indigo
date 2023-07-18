package did

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

var mrResolvedDidsTotal = promauto.NewCounterVec(prometheus.CounterOpts{
	Name: "multiresolver_resolved_dids_total",
	Help: "Total number of DIDs resolved",
}, []string{"resolver"})
