package main

import (
	"strings"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	dto "github.com/prometheus/client_model/go"
)

// filteredGatherer is a custom gatherer that filters metrics.
type filteredGatherer struct {
	originalGatherer prometheus.Gatherer
	filterPrefix     string
}

// NewFilteredGatherer creates a new instance of filteredGatherer.
func NewFilteredGatherer(gatherer prometheus.Gatherer, prefix string) prometheus.Gatherer {
	return &filteredGatherer{
		originalGatherer: gatherer,
		filterPrefix:     prefix,
	}
}

// Gather implements the prometheus.Gatherer interface and filters metrics with the given prefix.
func (fg *filteredGatherer) Gather() ([]*dto.MetricFamily, error) {
	metricFamilies, err := fg.originalGatherer.Gather()
	if err != nil {
		return nil, err
	}

	filteredMetricFamilies := make([]*dto.MetricFamily, 0, len(metricFamilies))
	for _, m := range metricFamilies {
		if !strings.HasPrefix(m.GetName(), fg.filterPrefix) {
			filteredMetricFamilies = append(filteredMetricFamilies, m)
		}
	}

	return filteredMetricFamilies, nil
}

var activeDBGauge = promauto.NewGauge(prometheus.GaugeOpts{
	Name: "literiver_active_dbs",
	Help: "Number of active databases",
})

var dbsSeenCounter = promauto.NewCounter(prometheus.CounterOpts{
	Name: "literiver_seen_dbs_total",
	Help: "Number of databases seen since startup",
})
