package repomgr

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

var repoOpsImported = promauto.NewCounter(prometheus.CounterOpts{
	Name: "repomgr_repo_ops_imported",
	Help: "Number of repo ops imported",
})

var openAndSigCheckDuration = promauto.NewHistogram(prometheus.HistogramOpts{
	Name:    "repomgr_open_and_sig_check_duration",
	Help:    "Duration of opening and signature check",
	Buckets: prometheus.ExponentialBuckets(0.001, 2, 15),
})

var calcDiffDuration = promauto.NewHistogram(prometheus.HistogramOpts{
	Name:    "repomgr_calc_diff_duration",
	Help:    "Duration of calculating diff",
	Buckets: prometheus.ExponentialBuckets(0.001, 2, 15),
})

var writeCarSliceDuration = promauto.NewHistogram(prometheus.HistogramOpts{
	Name:    "repomgr_write_car_slice_duration",
	Help:    "Duration of writing car slice",
	Buckets: prometheus.ExponentialBuckets(0.001, 2, 15),
})
