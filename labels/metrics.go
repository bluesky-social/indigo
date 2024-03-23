package labels

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

var labelsReceivedCounter = promauto.NewCounterVec(prometheus.CounterOpts{
	Name: "indigo_labels_received_total",
	Help: "Total number of labels received from label providers.",
}, []string{"provider"})
