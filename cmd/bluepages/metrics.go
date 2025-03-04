package main

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

var handleCacheHits = promauto.NewCounter(prometheus.CounterOpts{
	Name: "bluepages_resolve_handle_cache_hits",
	Help: "Number of cache hits for ATProto handle resolutions",
})

var handleCacheMisses = promauto.NewCounter(prometheus.CounterOpts{
	Name: "bluepages_resolve_handle_cache_misses",
	Help: "Number of cache misses for ATProto handle resolutions",
})

var handleRequestsCoalesced = promauto.NewCounter(prometheus.CounterOpts{
	Name: "bluepages_resolve_handle_requests_coalesced",
	Help: "Number of handle requests coalesced",
})

var handleResolutionErrors = promauto.NewCounter(prometheus.CounterOpts{
	Name: "bluepages_resolve_handle_resolution_errors",
	Help: "Number of non-cached handle resolution errors",
})

var handleResolveDuration = promauto.NewHistogramVec(prometheus.HistogramOpts{
	Name:    "bluepages_resolve_handle_duration",
	Help:    "Time to resolve a handle from network (not cached)",
	Buckets: prometheus.ExponentialBucketsRange(0.001, 2, 15),
}, []string{"status"})

var didCacheHits = promauto.NewCounter(prometheus.CounterOpts{
	Name: "bluepages_resolve_did_cache_hits",
	Help: "Number of cache hits for ATProto DID resolutions",
})

var didCacheMisses = promauto.NewCounter(prometheus.CounterOpts{
	Name: "bluepages_resolve_did_cache_misses",
	Help: "Number of cache misses for ATProto DID resolutions",
})

var didRequestsCoalesced = promauto.NewCounter(prometheus.CounterOpts{
	Name: "bluepages_resolve_did_requests_coalesced",
	Help: "Number of DID requests coalesced",
})

var didResolutionErrors = promauto.NewCounter(prometheus.CounterOpts{
	Name: "bluepages_resolve_did_resolution_errors",
	Help: "Number of non-cached DID resolution errors",
})

var didResolveDuration = promauto.NewHistogramVec(prometheus.HistogramOpts{
	Name:    "bluepages_resolve_did_duration",
	Help:    "Time to resolve a DID from network (not cached)",
	Buckets: prometheus.ExponentialBucketsRange(0.001, 2, 15),
}, []string{"status"})
