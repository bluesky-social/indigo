package apidir

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

var handleResolution = promauto.NewCounterVec(prometheus.CounterOpts{
	Name: "atproto_identity_apidir_resolve_handle",
	Help: "ATProto handle resolutions",
}, []string{"directory", "status"})

var handleResolutionDuration = promauto.NewHistogramVec(prometheus.HistogramOpts{
	Name:    "atproto_identity_apidir_resolve_handle_duration",
	Help:    "Time to resolve a handle",
	Buckets: prometheus.ExponentialBuckets(0.0001, 2, 20),
}, []string{"directory", "status"})

var didResolution = promauto.NewCounterVec(prometheus.CounterOpts{
	Name: "atproto_identity_apidir_resolve_did",
	Help: "ATProto DID resolutions",
}, []string{"directory", "status"})

var didResolutionDuration = promauto.NewHistogramVec(prometheus.HistogramOpts{
	Name:    "atproto_identity_apidir_resolve_did_duration",
	Help:    "Time to resolve a DID",
	Buckets: prometheus.ExponentialBuckets(0.0001, 2, 20),
}, []string{"directory", "status"})

var identityResolution = promauto.NewCounterVec(prometheus.CounterOpts{
	Name: "atproto_identity_apidir_resolve_identity",
	Help: "ATProto combined identity resolutions",
}, []string{"directory", "status"})

var identityResolutionDuration = promauto.NewHistogramVec(prometheus.HistogramOpts{
	Name:    "atproto_identity_apidir_resolve_identity_duration",
	Help:    "Time to resolve a combined identity",
	Buckets: prometheus.ExponentialBuckets(0.0001, 2, 20),
}, []string{"directory", "status"})
