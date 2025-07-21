package identity

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

// DEPRECATED
var handleCacheHits = promauto.NewCounter(prometheus.CounterOpts{
	Name: "atproto_directory_handle_cache_hits",
	Help: "Number of cache hits for ATProto handle lookups",
})

// DEPRECATED
var handleCacheMisses = promauto.NewCounter(prometheus.CounterOpts{
	Name: "atproto_directory_handle_cache_misses",
	Help: "Number of cache misses for ATProto handle lookups",
})

// DEPRECATED
var identityCacheHits = promauto.NewCounter(prometheus.CounterOpts{
	Name: "atproto_directory_identity_cache_hits",
	Help: "Number of cache hits for ATProto identity lookups",
})

// DEPRECATED
var identityCacheMisses = promauto.NewCounter(prometheus.CounterOpts{
	Name: "atproto_directory_identity_cache_misses",
	Help: "Number of cache misses for ATProto identity lookups",
})

// DEPRECATED
var identityRequestsCoalesced = promauto.NewCounter(prometheus.CounterOpts{
	Name: "atproto_directory_identity_requests_coalesced",
	Help: "Number of identity requests coalesced",
})

// DEPRECATED
var handleRequestsCoalesced = promauto.NewCounter(prometheus.CounterOpts{
	Name: "atproto_directory_handle_requests_coalesced",
	Help: "Number of handle requests coalesced",
})
