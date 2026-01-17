package metrics

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

const (
	namespace = "cask"
)

var (
	QueryDuration = promauto.NewHistogramVec(
		prometheus.HistogramOpts{
			Name:      "query_duration_seconds",
			Namespace: namespace,
			Help:      "Duration histogram of FoundationDB queries in seconds",
			Buckets:   prometheus.ExponentialBuckets(0.0001, 2, 18), // 0.1ms to ~13s
		},
		[]string{"query", "status"},
	)
)
