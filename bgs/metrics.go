package bgs

import (
	"errors"
	"net/http"
	"strconv"
	"time"

	"github.com/labstack/echo/v4"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

var eventsReceivedCounter = promauto.NewCounterVec(prometheus.CounterOpts{
	Name: "events_received_counter",
	Help: "The total number of events received",
}, []string{"pds"})

var eventsHandleDuration = promauto.NewHistogramVec(prometheus.HistogramOpts{
	Name:    "events_handle_duration",
	Help:    "A histogram of handleFedEvent latencies",
	Buckets: prometheus.ExponentialBuckets(0.001, 2, 15),
}, []string{"pds"})

var repoCommitsReceivedCounter = promauto.NewCounterVec(prometheus.CounterOpts{
	Name: "repo_commits_received_counter",
	Help: "The total number of events received",
}, []string{"pds"})

var repoCommitsResultCounter = promauto.NewCounterVec(prometheus.CounterOpts{
	Name: "repo_commits_result_counter",
	Help: "The results of commit events received",
}, []string{"pds", "status"})

var rebasesCounter = promauto.NewCounterVec(prometheus.CounterOpts{
	Name: "event_rebases",
	Help: "The total number of rebase events received",
}, []string{"pds"})

var eventsSentCounter = promauto.NewCounterVec(prometheus.CounterOpts{
	Name: "events_sent_counter",
	Help: "The total number of events sent to consumers",
}, []string{"remote_addr", "user_agent"})

var externalUserCreationAttempts = promauto.NewCounter(prometheus.CounterOpts{
	Name: "bgs_external_user_creation_attempts",
	Help: "The total number of external users created",
})

var connectedInbound = promauto.NewGauge(prometheus.GaugeOpts{
	Name: "bgs_connected_inbound",
	Help: "Number of inbound firehoses we are consuming",
})

var compactionDuration = promauto.NewHistogram(prometheus.HistogramOpts{
	Name:    "compaction_duration",
	Help:    "A histogram of compaction latencies",
	Buckets: prometheus.ExponentialBuckets(0.001, 3, 14),
})

var compactionQueueDepth = promauto.NewGauge(prometheus.GaugeOpts{
	Name: "compaction_queue_depth",
	Help: "The current depth of the compaction queue",
})

var newUsersDiscovered = promauto.NewCounter(prometheus.CounterOpts{
	Name: "bgs_new_users_discovered",
	Help: "The total number of new users discovered directly from the firehose (not from refs)",
})

var reqSz = promauto.NewHistogramVec(prometheus.HistogramOpts{
	Name:    "http_request_size_bytes",
	Help:    "A histogram of request sizes for requests.",
	Buckets: prometheus.ExponentialBuckets(100, 10, 8),
}, []string{"code", "method", "path"})

var reqDur = promauto.NewHistogramVec(prometheus.HistogramOpts{
	Name:    "http_request_duration_seconds",
	Help:    "A histogram of latencies for requests.",
	Buckets: prometheus.ExponentialBuckets(0.001, 2, 15),
}, []string{"code", "method", "path"})

var reqCnt = promauto.NewCounterVec(prometheus.CounterOpts{
	Name: "http_requests_total",
	Help: "A counter for requests to the wrapped handler.",
}, []string{"code", "method", "path"})

var resSz = promauto.NewHistogramVec(prometheus.HistogramOpts{
	Name:    "http_response_size_bytes",
	Help:    "A histogram of response sizes for requests.",
	Buckets: prometheus.ExponentialBuckets(100, 10, 8),
}, []string{"code", "method", "path"})

var userLookupDuration = promauto.NewHistogram(prometheus.HistogramOpts{
	Name:    "relay_user_lookup_duration",
	Help:    "A histogram of user lookup latencies",
	Buckets: prometheus.ExponentialBuckets(0.001, 2, 15),
})

var newUserDiscoveryDuration = promauto.NewHistogram(prometheus.HistogramOpts{
	Name:    "relay_new_user_discovery_duration",
	Help:    "A histogram of new user discovery latencies",
	Buckets: prometheus.ExponentialBuckets(0.001, 2, 15),
})

// MetricsMiddleware defines handler function for metrics middleware
func MetricsMiddleware(next echo.HandlerFunc) echo.HandlerFunc {
	return func(c echo.Context) error {
		path := c.Path()
		if path == "/metrics" || path == "/_health" {
			return next(c)
		}

		start := time.Now()
		requestSize := computeApproximateRequestSize(c.Request())

		err := next(c)

		status := c.Response().Status
		if err != nil {
			var httpError *echo.HTTPError
			if errors.As(err, &httpError) {
				status = httpError.Code
			}
			if status == 0 || status == http.StatusOK {
				status = http.StatusInternalServerError
			}
		}

		elapsed := float64(time.Since(start)) / float64(time.Second)

		statusStr := strconv.Itoa(status)
		method := c.Request().Method

		responseSize := float64(c.Response().Size)

		reqDur.WithLabelValues(statusStr, method, path).Observe(elapsed)
		reqCnt.WithLabelValues(statusStr, method, path).Inc()
		reqSz.WithLabelValues(statusStr, method, path).Observe(float64(requestSize))
		resSz.WithLabelValues(statusStr, method, path).Observe(responseSize)

		return err
	}
}

func computeApproximateRequestSize(r *http.Request) int {
	s := 0
	if r.URL != nil {
		s = len(r.URL.Path)
	}

	s += len(r.Method)
	s += len(r.Proto)
	for name, values := range r.Header {
		s += len(name)
		for _, value := range values {
			s += len(value)
		}
	}
	s += len(r.Host)

	// N.B. r.Form and r.MultipartForm are assumed to be included in r.URL.

	if r.ContentLength != -1 {
		s += int(r.ContentLength)
	}
	return s
}
