package main

import (
	"errors"
	"github.com/labstack/echo/v4"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"net/http"
	"strconv"
	"time"
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

var reqDur = promauto.NewHistogramVec(prometheus.HistogramOpts{
	Name:    "http_request_duration_seconds",
	Help:    "A histogram of latencies for requests.",
	Buckets: prometheus.ExponentialBuckets(0.001, 2, 15),
}, []string{"code", "method", "path"})

var reqCnt = promauto.NewCounterVec(prometheus.CounterOpts{
	Name: "http_requests_total",
	Help: "A counter for requests to the wrapped handler.",
}, []string{"code", "method", "path"})

// MetricsMiddleware defines handler function for metrics middleware
// TODO: reunify with bgs/metrics.go ?
func MetricsMiddleware(next echo.HandlerFunc) echo.HandlerFunc {
	return func(c echo.Context) error {
		path := c.Path()
		if path == "/metrics" || path == "/_health" {
			return next(c)
		}

		start := time.Now()

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

		reqDur.WithLabelValues(statusStr, method, path).Observe(elapsed)
		reqCnt.WithLabelValues(statusStr, method, path).Inc()

		return err
	}
}
