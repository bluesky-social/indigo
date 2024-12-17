package plc

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

var cacheHitsTotal = promauto.NewCounter(prometheus.CounterOpts{
	Name: "plc_cache_hits_total",
	Help: "Total number of cache hits",
})

var cacheMissesTotal = promauto.NewCounter(prometheus.CounterOpts{
	Name: "plc_cache_misses_total",
	Help: "Total number of cache misses",
})

var memcacheHitsTotal = promauto.NewCounter(prometheus.CounterOpts{
	Name: "plc_memcache_hits_total",
	Help: "Total number of cache hits",
})

var memcacheMissesTotal = promauto.NewCounter(prometheus.CounterOpts{
	Name: "plc_memcache_misses_total",
	Help: "Total number of cache misses",
})
