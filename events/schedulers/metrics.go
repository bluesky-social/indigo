package schedulers

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

var WorkItemsAdded = promauto.NewCounterVec(prometheus.CounterOpts{
	Name: "indigo_scheduler_work_items_added_total",
	Help: "Total number of work items added to the consumer pool",
}, []string{"pool", "scheduler_type"})

var WorkItemsProcessed = promauto.NewCounterVec(prometheus.CounterOpts{
	Name: "indigo_scheduler_work_items_processed_total",
	Help: "Total number of work items processed by the consumer pool",
}, []string{"pool", "scheduler_type"})

var WorkItemsActive = promauto.NewCounterVec(prometheus.CounterOpts{
	Name: "indigo_scheduler_work_items_active_total",
	Help: "Total number of work items passed into a worker",
}, []string{"pool", "scheduler_type"})

var WorkersActive = promauto.NewGaugeVec(prometheus.GaugeOpts{
	Name: "indigo_scheduler_workers_active",
	Help: "Number of workers currently active",
}, []string{"pool", "scheduler_type"})
