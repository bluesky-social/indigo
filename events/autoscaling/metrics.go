package autoscaling

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

var workItemsAdded = promauto.NewCounterVec(prometheus.CounterOpts{
	Name: "indigo_pool_work_items_added_total",
	Help: "Total number of work items added to the consumer pool",
}, []string{"pool", "pool_type"})

var workItemsProcessed = promauto.NewCounterVec(prometheus.CounterOpts{
	Name: "indigo_pool_work_items_processed_total",
	Help: "Total number of work items processed by the consumer pool",
}, []string{"pool", "pool_type"})

var workItemsActive = promauto.NewCounterVec(prometheus.CounterOpts{
	Name: "indigo_pool_work_items_active_total",
	Help: "Total number of work items passed into a worker",
}, []string{"pool", "pool_type"})

var workersActive = promauto.NewGaugeVec(prometheus.GaugeOpts{
	Name: "indigo_pool_workers_active",
	Help: "Number of workers currently active",
}, []string{"pool", "pool_type"})
