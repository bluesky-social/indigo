package querycheck

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

var execCounter = promauto.NewCounterVec(prometheus.CounterOpts{
	Name: "querycheck_exec_total",
	Help: "total number of executions since starting the querycheck",
}, []string{"query"})

var execDurationCounter = promauto.NewCounterVec(prometheus.CounterOpts{
	Name: "querycheck_exec_duration_ms_total",
	Help: "total ms spent executing the query since starting the querycheck",
}, []string{"query"})

var errorCounter = promauto.NewCounterVec(prometheus.CounterOpts{
	Name: "querycheck_errors_total",
	Help: "number of errors encountered since starting the querycheck",
}, []string{"query"})

var blocksHitCounter = promauto.NewCounterVec(prometheus.CounterOpts{
	Name: "querycheck_blocks_hit_total",
	Help: "blocks hit total since starting the querycheck",
}, []string{"query"})

var blocksReadCounter = promauto.NewCounterVec(prometheus.CounterOpts{
	Name: "querycheck_blocks_read_total",
	Help: "blocks read total since starting the querycheck",
}, []string{"query"})

var blocksWrittenCounter = promauto.NewCounterVec(prometheus.CounterOpts{
	Name: "querycheck_blocks_written_total",
	Help: "blocks written total since starting the querycheck",
}, []string{"query"})

var blocksDirtyCounter = promauto.NewCounterVec(prometheus.CounterOpts{
	Name: "querycheck_blocks_dirty_total",
	Help: "blocks dirty total since starting the querycheck",
}, []string{"query"})

var ioReadTimeCounter = promauto.NewCounterVec(prometheus.CounterOpts{
	Name: "querycheck_io_read_ms_total",
	Help: "io read time (in ms) total since starting the querycheck",
}, []string{"query"})

var ioWriteTimeCounter = promauto.NewCounterVec(prometheus.CounterOpts{
	Name: "querycheck_io_write_ms_total",
	Help: "io write time (in ms) total since starting the querycheck",
}, []string{"query"})

var tempWrittenBlocksCounter = promauto.NewCounterVec(prometheus.CounterOpts{
	Name: "querycheck_temp_written_blocks_total",
	Help: "temp written blocks total since starting the querycheck",
}, []string{"query"})

var planNodeCounter = promauto.NewCounterVec(prometheus.CounterOpts{
	Name: "querycheck_plan_node_count_total",
	Help: "plan node count total since starting the querycheck",
}, []string{"query", "node_type"})
