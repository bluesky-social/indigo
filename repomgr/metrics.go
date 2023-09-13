package repomgr

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

var repoOpsImported = promauto.NewCounter(prometheus.CounterOpts{
	Name: "repomgr_repo_ops_imported",
	Help: "Number of repo ops imported",
})
