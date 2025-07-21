package main

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

var firehoseReceivedCounter = promauto.NewCounter(prometheus.CounterOpts{
	Name: "collectiondir_firehose_received_total",
	Help: "number of events received from upstream firehose",
})
var firehoseCommits = promauto.NewCounter(prometheus.CounterOpts{
	Name: "collectiondir_firehose_commits",
	Help: "number of #commit events received from upstream firehose",
})
var firehoseCommitOps = promauto.NewCounterVec(prometheus.CounterOpts{
	Name: "collectiondir_firehose_commit_ops",
	Help: "number of #commit events received from upstream firehose",
}, []string{"op"})

var firehoseDidcSet = promauto.NewCounter(prometheus.CounterOpts{
	Name: "collectiondir_firehose_didc_total",
})

var pebbleDup = promauto.NewCounter(prometheus.CounterOpts{
	Name: "collectiondir_pebble_dup_total",
})

var pebbleNew = promauto.NewCounter(prometheus.CounterOpts{
	Name: "collectiondir_pebble_new_total",
})

var pdsCrawledCounter = promauto.NewCounter(prometheus.CounterOpts{
	Name: "collectiondir_pds_crawled_total",
})

var pdsRepoPages = promauto.NewCounter(prometheus.CounterOpts{
	Name: "collectiondir_pds_repo_pages",
})

var pdsReposDescribed = promauto.NewCounter(prometheus.CounterOpts{
	Name: "collectiondir_pds_repos_described",
})

var statsCalculations = promauto.NewHistogram(prometheus.HistogramOpts{
	Name:    "collectiondir_stats_calculations",
	Help:    "how long it takes to calculate total stats",
	Buckets: prometheus.ExponentialBuckets(0.01, 2, 13),
})
