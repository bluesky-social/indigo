package identity

import (
	"context"
	"net"
	"net/http"
	"time"

	"github.com/bluesky-social/indigo/util/ssrf"

	"github.com/earthboundkid/versioninfo/v2"
	"golang.org/x/time/rate"
)

const (
	DefaultPLCURL                = "https://plc.directory"
	DefaultHTTPTimeout           = time.Second * 10
	DefaultPLCTimeout            = time.Second * 10
	DefaultTryAuthoritativeDNS   = true
	DefaultCacheCapacity         = 250_000
	DefaultCacheHitTTL           = time.Hour * 24
	DefaultCacheErrTTL           = time.Minute * 2
	DefaultCacheInvalidHandleTTL = time.Minute * 5
)

var (
	DefaultSkipHandleDNSSuffixes = []string{".bsky.social"}
	DefaultUserAgent             = "indigo-identity/" + versioninfo.Short()
)

type dirConfig struct {
	plcURL      string
	httpTimeout time.Duration
	plcTimeout  time.Duration
	userAgent   string
	plcLimiter  *rate.Limiter
	// XXX: didWebLimitFunc        func(ctx context.Context, hostname string) error
	// XXX: httpClient             *http.Client
	// XXX: plcClient              *http.Client
	skipHandleVerification bool
	tryAuthoritativeDNS    bool
	skipDNSSuffixes        []string
	cacheCapacity          int
	cacheHitTTL            time.Duration
	cacheErrTTL            time.Duration
	cacheInvalidHandleTTL  time.Duration
}

func defaultConfig() *dirConfig {
	return &dirConfig{
		plcURL:      DefaultPLCURL,
		httpTimeout: DefaultHTTPTimeout,
	}
}

// helper to ensure all values are in safe ranges (eg, no negative timeouts)
func (cfg *dirConfig) ensureValid() {
	// XXX: checks
}

type Option func(*dirConfig)

func NewDirectory(opts ...Option) *CacheDirectory {
	cfg := defaultConfig()
	for _, opt := range opts {
		opt(cfg)
	}

	cfg.ensureValid()

	ssrfHTTPDialer := ssrf.PublicOnlyDialer()
	base := &BaseDirectory{
		PLCURL: cfg.plcURL,
		HTTPClient: http.Client{
			Timeout: cfg.httpTimeout,
			Transport: &http.Transport{
				Proxy:             http.ProxyFromEnvironment,
				DialContext:       ssrfHTTPDialer.DialContext,
				ForceAttemptHTTP2: true,
				MaxIdleConns:      100,
				// would want this around 100ms for services doing lots of handle resolution
				IdleConnTimeout:       time.Millisecond * 1000,
				TLSHandshakeTimeout:   10 * time.Second,
				ExpectContinueTimeout: 1 * time.Second,
			},
		},
		PLCClient: &http.Client{
			Timeout: cfg.plcTimeout,
		},
		Resolver: net.Resolver{
			Dial: func(ctx context.Context, network, address string) (net.Conn, error) {
				d := net.Dialer{Timeout: time.Second * 3}
				return d.DialContext(ctx, network, address)
			},
		},
		TryAuthoritativeDNS:    cfg.tryAuthoritativeDNS,
		SkipDNSDomainSuffixes:  cfg.skipDNSSuffixes,
		SkipHandleVerification: cfg.skipHandleVerification,
		UserAgent:              cfg.userAgent,
	}

	cacheDir := NewCacheDirectory(base, cfg.cacheCapacity, cfg.cacheHitTTL, cfg.cacheErrTTL, cfg.cacheInvalidHandleTTL)

	return cacheDir
}

func WithPLCURL(val string) Option {
	return func(cfg *dirConfig) {
		cfg.plcURL = val
	}
}

func WithPLCLimiter(limiter *rate.Limiter) Option {
	return func(cfg *dirConfig) {
		cfg.plcLimiter = limiter
	}
}

func WithUserAgent(val string) Option {
	return func(cfg *dirConfig) {
		cfg.userAgent = val
	}
}

func WithHTTPTimeout(val time.Duration) Option {
	return func(cfg *dirConfig) {
		cfg.httpTimeout = val
	}
}

func WithSkipHandleVerification(val bool) Option {
	return func(cfg *dirConfig) {
		cfg.skipHandleVerification = val
	}
}

func WithTryAuthoritativeDNS(val bool) Option {
	return func(cfg *dirConfig) {
		cfg.tryAuthoritativeDNS = val
	}
}
