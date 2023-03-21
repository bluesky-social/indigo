package util

import (
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

// re-writes HTTP client DEBUG to INFO level (this is where retry is logged)
func (l LeveledZap) Debug(msg string, keysAndValues ...interface{}) {
	l.inner.Infow(msg, keysAndValues...)
}

// Generates an HTTP client with decent general-purpose defaults around
// timeouts and retries. The returned client has the stdlib http.Client
// interface, but has Hashicorp retryablehttp logic internally.
//
// This client will retry on connection errors, 5xx status (except 501), and
// 429 Backoff requests (respecting 'Retry-After' header). It will log
// intermediate failures with WARN level. This does not start from
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
	client := retryClient.StandardClient()
	client.Timeout = 20 * time.Second
	return client
}
