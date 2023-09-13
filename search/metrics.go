package search

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

var postsReceived = promauto.NewCounter(prometheus.CounterOpts{
	Name: "search_posts_received",
	Help: "Number of posts received",
})

var postsIndexed = promauto.NewCounter(prometheus.CounterOpts{
	Name: "search_posts_indexed",
	Help: "Number of posts indexed",
})

var postsFailed = promauto.NewCounter(prometheus.CounterOpts{
	Name: "search_posts_failed",
	Help: "Number of posts that failed indexing",
})

var postsDeleted = promauto.NewCounter(prometheus.CounterOpts{
	Name: "search_posts_deleted",
	Help: "Number of posts deleted",
})

var profilesReceived = promauto.NewCounter(prometheus.CounterOpts{
	Name: "search_profiles_received",
	Help: "Number of profiles received",
})

var profilesIndexed = promauto.NewCounter(prometheus.CounterOpts{
	Name: "search_profiles_indexed",
	Help: "Number of profiles indexed",
})

var profilesFailed = promauto.NewCounter(prometheus.CounterOpts{
	Name: "search_profiles_failed",
	Help: "Number of profiles that failed indexing",
})

var profilesDeleted = promauto.NewCounter(prometheus.CounterOpts{
	Name: "search_profiles_deleted",
	Help: "Number of profiles deleted",
})

var currentSeq = promauto.NewGauge(prometheus.GaugeOpts{
	Name: "search_current_seq",
	Help: "Current sequence number",
})
