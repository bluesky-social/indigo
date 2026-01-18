package metrics

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

const (
	namespace = "cask"
)

// Subscriber metrics
var (
	// ActiveSubscribers tracks the current number of connected WebSocket subscribers
	ActiveSubscribers = promauto.NewGauge(prometheus.GaugeOpts{
		Name:      "active_subscribers",
		Namespace: namespace,
		Help:      "Current number of active WebSocket subscribers",
	})

	// EventsSentTotal tracks the total number of events sent to subscribers
	EventsSentTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Name:      "events_sent_total",
		Namespace: namespace,
		Help:      "Total number of events sent to subscribers",
	}, []string{"remote_addr", "user_agent"})

	// SubscriberConnections tracks the total number of subscriber connections (including disconnects)
	SubscriberConnections = promauto.NewCounter(prometheus.CounterOpts{
		Name:      "subscriber_connections_total",
		Namespace: namespace,
		Help:      "Total number of subscriber connections",
	})
)

// Status label values
const (
	StatusOK    = "ok"
	StatusError = "error"
)

// Consumer metrics (upstream firehose)
var (
	// EventsReceivedTotal tracks the total number of events received from the upstream firehose
	EventsReceivedTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Name:      "events_received_total",
		Namespace: namespace,
		Help:      "Total number of events received from the upstream firehose",
	}, []string{"event_type", "status"})

	// EventSizeBytes tracks the distribution of event sizes in bytes
	// Buckets: 128, 256, 512, 1KB, 2KB, 4KB, 8KB, 16KB, 32KB, 64KB, 128KB, 256KB, 512KB, 1MB
	EventSizeBytes = promauto.NewHistogram(prometheus.HistogramOpts{
		Name:      "event_size_bytes",
		Namespace: namespace,
		Help:      "Distribution of event sizes in bytes",
		Buckets:   prometheus.ExponentialBuckets(128, 2, 14),
	})

	// UpstreamSeq tracks the latest upstream sequence number processed
	UpstreamSeq = promauto.NewGauge(prometheus.GaugeOpts{
		Name:      "upstream_seq",
		Namespace: namespace,
		Help:      "Latest upstream sequence number processed",
	})

	// ConsumerConnected indicates whether the consumer is currently connected to the upstream
	ConsumerConnected = promauto.NewGauge(prometheus.GaugeOpts{
		Name:      "consumer_connected",
		Namespace: namespace,
		Help:      "Whether the consumer is currently connected to the upstream firehose (1=connected, 0=disconnected)",
	})
)
