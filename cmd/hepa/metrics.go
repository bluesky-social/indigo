package main

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"go.opentelemetry.io/otel"
)

var tracer = otel.Tracer("hepa")

var postsReceived = promauto.NewCounter(prometheus.CounterOpts{
	Name: "hepa_posts_received",
	Help: "Number of posts received",
})

var postsIndexed = promauto.NewCounter(prometheus.CounterOpts{
	Name: "hepa_posts_indexed",
	Help: "Number of posts indexed",
})

var postsFailed = promauto.NewCounter(prometheus.CounterOpts{
	Name: "hepa_posts_failed",
	Help: "Number of posts that failed indexing",
})

var postsDeleted = promauto.NewCounter(prometheus.CounterOpts{
	Name: "hepa_posts_deleted",
	Help: "Number of posts deleted",
})

var profilesReceived = promauto.NewCounter(prometheus.CounterOpts{
	Name: "hepa_profiles_received",
	Help: "Number of profiles received",
})

var profilesIndexed = promauto.NewCounter(prometheus.CounterOpts{
	Name: "hepa_profiles_indexed",
	Help: "Number of profiles indexed",
})

var profilesFailed = promauto.NewCounter(prometheus.CounterOpts{
	Name: "hepa_profiles_failed",
	Help: "Number of profiles that failed indexing",
})

var profilesDeleted = promauto.NewCounter(prometheus.CounterOpts{
	Name: "hepa_profiles_deleted",
	Help: "Number of profiles deleted",
})

var currentSeq = promauto.NewGauge(prometheus.GaugeOpts{
	Name: "hepa_current_seq",
	Help: "Current sequence number",
})
