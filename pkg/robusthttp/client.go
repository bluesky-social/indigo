package robusthttp

import (
	"context"
	"log/slog"
	"net/http"
	"time"

	"github.com/hashicorp/go-cleanhttp"
	"github.com/hashicorp/go-retryablehttp"
	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
)

type LeveledSlog struct {
	inner *slog.Logger
}

// re-writes HTTP client ERROR to WARN level (because of retries)
func (l LeveledSlog) Error(msg string, keysAndValues ...any) {
	l.inner.Warn(msg, keysAndValues...)
}

func (l LeveledSlog) Warn(msg string, keysAndValues ...any) {
	l.inner.Warn(msg, keysAndValues...)
}

func (l LeveledSlog) Info(msg string, keysAndValues ...any) {
	l.inner.Info(msg, keysAndValues...)
}

func (l LeveledSlog) Debug(msg string, keysAndValues ...any) {
	l.inner.Debug(msg, keysAndValues...)
}

type Option func(*retryablehttp.Client)

// WithMaxRetries sets the maximum number of retries for the HTTP client.
func WithMaxRetries(maxRetries int) Option {
	return func(client *retryablehttp.Client) {
		client.RetryMax = maxRetries
	}
}

// WithRetryWaitMin sets the minimum wait time between retries.
func WithRetryWaitMin(waitMin time.Duration) Option {
	return func(client *retryablehttp.Client) {
		client.RetryWaitMin = waitMin
	}
}

// WithRetryWaitMax sets the maximum wait time between retries.
func WithRetryWaitMax(waitMax time.Duration) Option {
	return func(client *retryablehttp.Client) {
		client.RetryWaitMax = waitMax
	}
}

// WithLogger sets a custom logger for the HTTP client.
func WithLogger(logger *slog.Logger) Option {
	return func(client *retryablehttp.Client) {
		client.Logger = retryablehttp.LeveledLogger(LeveledSlog{inner: logger})
	}
}

// WithTransport sets a custom transport for the HTTP client.
func WithTransport(transport http.RoundTripper) Option {
	return func(client *retryablehttp.Client) {
		client.HTTPClient.Transport = transport
	}
}

// WithRetryPolicy sets a custom retry policy for the HTTP client.
func WithRetryPolicy(policy retryablehttp.CheckRetry) Option {
	return func(client *retryablehttp.Client) {
		client.CheckRetry = policy
	}
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
func NewClient(options ...Option) *http.Client {
	logger := LeveledSlog{inner: slog.Default().With("subsystem", "RobustHTTPClient")}
	retryClient := retryablehttp.NewClient()
	retryClient.HTTPClient.Transport = otelhttp.NewTransport(cleanhttp.DefaultPooledTransport())
	retryClient.RetryMax = 3
	retryClient.RetryWaitMin = 1 * time.Second
	retryClient.RetryWaitMax = 10 * time.Second
	retryClient.Logger = retryablehttp.LeveledLogger(logger)
	retryClient.CheckRetry = DefaultRetryPolicy

	for _, option := range options {
		option(retryClient)
	}

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

// DefaultRetryPolicy is a custom wrapper around retryablehttp.DefaultRetryPolicy.
// It treats `429 Too Many Requests` as non-retryable, so the application can decide
// how to deal with rate-limiting.
func DefaultRetryPolicy(ctx context.Context, resp *http.Response, err error) (bool, error) {
	if err == nil && resp.StatusCode == http.StatusTooManyRequests {
		return false, nil
	}
	return retryablehttp.DefaultRetryPolicy(ctx, resp, err)
}

func NoInternalServerErrorPolicy(ctx context.Context, resp *http.Response, err error) (bool, error) {
	if err == nil && resp.StatusCode == http.StatusTooManyRequests {
		return false, nil
	}
	if err == nil && resp.StatusCode == http.StatusInternalServerError {
		return false, nil
	}
	return retryablehttp.DefaultRetryPolicy(ctx, resp, err)
}
