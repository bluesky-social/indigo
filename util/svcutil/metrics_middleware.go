package svcutil

import (
	"errors"
	"net/http"
	"strconv"
	"time"

	"github.com/labstack/echo/v4"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

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
