package identity

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

var handleResolution = promauto.NewCounterVec(prometheus.CounterOpts{
	Name: "atproto_identity_resolve_handle",
	Help: "ATProto handle resolutions",
}, []string{"directory", "status"})

var handleResolutionDuration = promauto.NewHistogramVec(prometheus.HistogramOpts{
	Name:    "atproto_identity_resolve_handle_duration",
	Help:    "Time to resolve a handle",
	Buckets: prometheus.ExponentialBucketsRange(0.0001, 2, 20),
}, []string{"directory", "status"})

var didResolution = promauto.NewCounterVec(prometheus.CounterOpts{
	Name: "atproto_identity_resolve_did",
	Help: "ATProto DID resolutions",
}, []string{"directory", "status"})

var didResolutionDuration = promauto.NewHistogramVec(prometheus.HistogramOpts{
	Name:    "atproto_identity_resolve_did_duration",
	Help:    "Time to resolve a DID",
	Buckets: prometheus.ExponentialBucketsRange(0.0001, 2, 20),
}, []string{"directory", "status"})
