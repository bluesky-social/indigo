package main

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

var handleCacheHits = promauto.NewCounter(prometheus.CounterOpts{
	Name: "atproto_redis_resolver_handle_cache_hits",
	Help: "Number of cache hits for ATProto handle resolutions",
})

var handleCacheMisses = promauto.NewCounter(prometheus.CounterOpts{
	Name: "atproto_redis_resolver_handle_cache_misses",
	Help: "Number of cache misses for ATProto handle resolutions",
})

var handleRequestsCoalesced = promauto.NewCounter(prometheus.CounterOpts{
	Name: "atproto_redis_resolver_handle_requests_coalesced",
	Help: "Number of handle requests coalesced",
})

var didCacheHits = promauto.NewCounter(prometheus.CounterOpts{
	Name: "atproto_redis_resolver_did_cache_hits",
	Help: "Number of cache hits for ATProto DID resolutions",
})

var didCacheMisses = promauto.NewCounter(prometheus.CounterOpts{
	Name: "atproto_redis_resolver_did_cache_misses",
	Help: "Number of cache misses for ATProto DID resolutions",
})

var didRequestsCoalesced = promauto.NewCounter(prometheus.CounterOpts{
	Name: "atproto_redis_resolver_did_requests_coalesced",
	Help: "Number of DID requests coalesced",
})
