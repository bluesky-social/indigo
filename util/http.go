package util

import (
	"context"
	"net/http"
	"time"

	"github.com/hashicorp/go-retryablehttp"
	logging "github.com/ipfs/go-log"
)

var log = logging.Logger("http")

type LeveledZap struct {
	inner *logging.ZapEventLogger
}

// re-writes HTTP client ERROR to WARN level (because of retries)
func (l LeveledZap) Error(msg string, keysAndValues ...interface{}) {
	l.inner.Warnw(msg, keysAndValues...)
}

func (l LeveledZap) Warn(msg string, keysAndValues ...interface{}) {
	l.inner.Warnw(msg, keysAndValues...)
}

func (l LeveledZap) Info(msg string, keysAndValues ...interface{}) {
	l.inner.Infow(msg, keysAndValues...)
}

func (l LeveledZap) Debug(msg string, keysAndValues ...interface{}) {
	l.inner.Debugw(msg, keysAndValues...)
}

// Generates an HTTP client with decent general-purpose defaults around
// timeouts and retries. The returned client has the stdlib http.Client
// interface, but has Hashicorp retryablehttp logic internally.
//
// This client will retry on connection errors, 5xx status (except 501).
// It will log intermediate failures with WARN level. This does not start from
// http.DefaultClient.
//
// This should be usable for XRPC clients, and other general inter-service
// client needs. CLI tools might want shorter timeouts and fewer retries by
// default.
func RobustHTTPClient() *http.Client {

	retryClient := retryablehttp.NewClient()
	retryClient.RetryMax = 3
	retryClient.RetryWaitMin = 1 * time.Second
	retryClient.RetryWaitMax = 10 * time.Second
	retryClient.Logger = retryablehttp.LeveledLogger(LeveledZap{log})
	retryClient.CheckRetry = XRPCRetryPolicy
	client := retryClient.StandardClient()
	client.Timeout = 30 * time.Second
	return client
}

// For use in local integration tests. Short timeouts, no retries, etc
func TestingHTTPClient() *http.Client {

	client := http.DefaultClient
	client.Timeout = 1 * time.Second
	return client
}

// XRPCRetryPolicy is a custom wrapper around retryablehttp.DefaultRetryPolicy.
// It treats `429 Too Many Requests` as non-retryable, so the application can decide
// how to deal with rate-limiting.
func XRPCRetryPolicy(ctx context.Context, resp *http.Response, err error) (bool, error) {
	if err == nil && resp.StatusCode == http.StatusTooManyRequests {
		return false, nil
	}
	// TODO: implement returning errors on non-200 responses w/o introducing circular dependencies.
	return retryablehttp.DefaultRetryPolicy(ctx, resp, err)
}
