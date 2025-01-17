package carstore

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

var writeShardFileDuration = promauto.NewHistogram(prometheus.HistogramOpts{
	Name:    "carstore_write_shard_file_duration",
	Help:    "Duration of writing shard file to disk",
	Buckets: prometheus.ExponentialBuckets(0.001, 2, 15),
})

var writeShardMetadataDuration = promauto.NewHistogram(prometheus.HistogramOpts{
	Name:    "carstore_write_shard_metadata_duration",
	Help:    "Duration of writing shard metadata to DB",
	Buckets: prometheus.ExponentialBuckets(0.001, 2, 15),
})
