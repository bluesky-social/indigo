package identity

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

var handleCacheHits = promauto.NewCounter(prometheus.CounterOpts{
	Name: "atproto_directory_handle_cache_hits",
	Help: "Number of cache hits for ATProto handle lookups",
})

var handleCacheMisses = promauto.NewCounter(prometheus.CounterOpts{
	Name: "atproto_directory_handle_cache_misses",
	Help: "Number of cache misses for ATProto handle lookups",
})

var identityCacheHits = promauto.NewCounter(prometheus.CounterOpts{
	Name: "atproto_directory_identity_cache_hits",
	Help: "Number of cache hits for ATProto identity lookups",
})

var identityCacheMisses = promauto.NewCounter(prometheus.CounterOpts{
	Name: "atproto_directory_identity_cache_misses",
	Help: "Number of cache misses for ATProto identity lookups",
})

var identityRequestsCoalesced = promauto.NewCounter(prometheus.CounterOpts{
	Name: "atproto_directory_identity_requests_coalesced",
	Help: "Number of identity requests coalesced",
})

var handleRequestsCoalesced = promauto.NewCounter(prometheus.CounterOpts{
	Name: "atproto_directory_handle_requests_coalesced",
	Help: "Number of handle requests coalesced",
})

var lookupDuration = promauto.NewHistogramVec(prometheus.HistogramOpts{
	Name:    "atproto_directory_lookup_duration_seconds",
	Help:    "Duration of DNS lookups",
	Buckets: prometheus.DefBuckets,
}, []string{"type", "status"})
