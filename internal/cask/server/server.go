package server

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"strings"
	"sync"
	"time"

	_ "net/http/pprof"

	"github.com/bluesky-social/indigo/internal/cask/models"
	"github.com/bluesky-social/indigo/pkg/foundation"
	"github.com/bluesky-social/indigo/pkg/foundation/leader"
	"github.com/bluesky-social/indigo/pkg/metrics"
	"github.com/bluesky-social/indigo/util"
	"github.com/bluesky-social/indigo/util/svcutil"
	"github.com/bluesky-social/indigo/xrpc"
	"github.com/google/uuid"
	"github.com/labstack/echo/v4"
	"github.com/labstack/echo/v4/middleware"
	"go.opentelemetry.io/otel"
	"golang.org/x/sync/errgroup"
)

type Config struct {
	Logger            *slog.Logger
	FDBClusterFile    string
	FirehoseURL       string
	ProxyHost         string
	CollectionDirHost string
	UserAgent         string
	NextCrawlers      []string
}

type Server struct {
	cfg Config
	log *slog.Logger

	echo            *echo.Echo
	metricsServer   *http.Server
	httpClient      *http.Client // For upstream proxy requests (no redirect following)
	peerClient      *http.Client // For next-crawler forwarding (robust client)
	nextCrawlerURLs []string

	db             *foundation.DB
	models         *models.Models
	leaderElection *leader.LeaderElection

	consumerMu     *sync.Mutex
	consumerCancel context.CancelFunc

	// Subscriber tracking
	subscribersMu    *sync.Mutex
	subscribers      map[uint64]*subscriber
	nextSubscriberID uint64
}

func New(ctx context.Context, config Config) (*Server, error) {
	const service = "cask"
	if err := metrics.InitTracing(ctx, service); err != nil {
		return nil, fmt.Errorf("failed to init tracing: %w", err)
	}

	db, err := foundation.New(ctx, service, &foundation.Config{
		Tracer:          otel.Tracer(service),
		APIVersion:      730,
		ClusterFilePath: config.FDBClusterFile,
		RetryLimit:      100,
	})
	if err != nil {
		return nil, err
	}

	m, err := models.New(db)
	if err != nil {
		return nil, fmt.Errorf("failed to init models: %w", err)
	}

	// Validate and store next-crawler URLs
	var nextCrawlers []string
	for _, raw := range config.NextCrawlers {
		if raw == "" {
			continue
		}
		u, err := url.Parse(raw)
		if err != nil {
			return nil, fmt.Errorf("failed to parse next-crawler url %q: %w", raw, err)
		}
		if u.Host == "" {
			return nil, fmt.Errorf("empty URL host for next crawler: %s", raw)
		}
		nextCrawlers = append(nextCrawlers, raw)
	}

	// HTTP client for upstream proxy requests - disable automatic redirect following
	upstreamClient := &http.Client{
		Timeout: 30 * time.Second,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}

	s := &Server{
		cfg:             config,
		log:             config.Logger,
		httpClient:      upstreamClient,
		peerClient:      util.RobustHTTPClient(),
		nextCrawlerURLs: nextCrawlers,
		db:              db,
		models:          m,
		consumerMu:      &sync.Mutex{},
		subscribersMu:   &sync.Mutex{},
	}

	s.leaderElection, err = leader.New(db, []string{"firehoseLeader"}, leader.LeaderElectionConfig{
		ID:               s.processID(),
		Logger:           config.Logger,
		OnBecameLeader:   s.onBecameLeader,
		OnLostLeadership: s.onLostLeadership,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to init leader election: %w", err)
	}

	return s, nil
}

func (s *Server) Start(ctx context.Context, addr string) error {
	go func() {
		if err := s.leaderElection.Run(ctx); err != nil && ctx.Err() == nil {
			s.log.Error("leader election stopped unexpectedly", "error", err)
		}
	}()

	s.echo = s.router()
	return s.echo.Start(addr)
}

func (s *Server) router() *echo.Echo {
	e := echo.New()
	e.HideBanner = true
	e.HidePort = true

	// CORS middleware - allow all origins for browser-based clients
	e.Use(middleware.CORSWithConfig(middleware.CORSConfig{
		AllowOrigins: []string{"*"},
		AllowHeaders: []string{echo.HeaderOrigin, echo.HeaderContentType, echo.HeaderAccept, echo.HeaderAuthorization},
	}))

	// Custom Server header middleware
	if s.cfg.UserAgent != "" {
		e.Use(func(next echo.HandlerFunc) echo.HandlerFunc {
			return func(c echo.Context) error {
				c.Response().Header().Set(echo.HeaderServer, s.cfg.UserAgent)
				return next(c)
			}
		})
	}

	e.Use(svcutil.MetricsMiddleware)
	e.HTTPErrorHandler = s.errorHandler

	// misc. handlers
	e.GET("/", s.handleHome)
	e.GET("/ping", s.handleHealth)
	e.GET("/_health", s.handleHealth)

	// xrpc handlers
	e.GET("/xrpc/_health", s.handleHealth)
	e.GET("/xrpc/com.atproto.sync.subscribeRepos", s.handleSubscribeRepos)
	e.GET("/xrpc/com.atproto.sync.listReposByCollection", s.proxyToCollectionDir)

	// requestCrawl - either forward to multiple crawlers or just proxy to upstream
	if len(s.nextCrawlerURLs) > 0 {
		e.POST("/xrpc/com.atproto.sync.requestCrawl", s.handleRequestCrawl)
	} else {
		e.POST("/xrpc/com.atproto.sync.requestCrawl", s.proxyToUpstream)
	}

	// Proxy all other xrpc and admin requests to upstream
	e.Any("/xrpc/*", s.proxyToUpstream)
	e.Any("/admin/*", s.proxyToUpstream)

	return e
}

// Gracefully stops the server process. If we're the leaseholder of the firehose consumer
// lock, we stop the consumer and release the lease. Then we close all subscriber connections
// gracefully before shutting down the HTTP server.
func (s *Server) Shutdown(ctx context.Context) error {
	s.stopConsumer()
	s.leaderElection.Stop()

	s.closeAllSubscribers()

	errs := errgroup.Group{}
	errs.Go(func() error {
		s.echo.Server.SetKeepAlivesEnabled(false)
		if err := s.echo.Shutdown(ctx); err != nil {
			s.log.Error("error shutting down API server", "error", err)
			return err
		}
		return nil
	})

	errs.Go(func() error {
		if s.metricsServer != nil {
			s.metricsServer.SetKeepAlivesEnabled(false)
			if err := s.metricsServer.Shutdown(ctx); err != nil {
				s.log.Error("error shutting down metrics server", "error", err)
				return err
			}
		}
		return nil
	})

	return errs.Wait()
}

func (s *Server) errorHandler(err error, c echo.Context) {
	code := http.StatusInternalServerError
	errStr := "InternalServerError"
	msg := "internal server error"

	if he, ok := err.(*echo.HTTPError); ok {
		code = he.Code
		if m, ok := he.Message.(string); ok {
			msg = m
		}
		switch code {
		case http.StatusBadRequest:
			errStr = "BadRequest"
		case http.StatusUnauthorized:
			errStr = "AuthRequired"
		case http.StatusForbidden:
			errStr = "Forbidden"
		case http.StatusNotFound:
			errStr = "NotFound"
		}
	}

	path := c.Path()

	if code >= 500 {
		s.log.Error("handler error", "path", path, "error", err)
	}

	// Don't send response for WebSocket paths - the connection is already upgraded
	if strings.Contains(path, "subscribeRepos") {
		return
	}

	if c.Response().Committed {
		return
	}

	// For admin paths, include the actual error message
	if strings.HasPrefix(path, "/admin/") {
		_ = c.JSON(code, map[string]any{"error": err.Error()})
		return
	}

	if err := c.JSON(code, xrpc.XRPCError{ErrStr: errStr, Message: msg}); err != nil {
		s.log.Error("failed to write error response", "error", err)
	}
}

func (s *Server) handleHealth(c echo.Context) error {
	return c.JSON(http.StatusOK, map[string]string{
		"service": "cask",
		"status":  "ok",
	})
}

func (s *Server) processID() string {
	hostname, err := os.Hostname()
	if err != nil {
		hostname = "unknown"
	}
	return fmt.Sprintf("%s-%s", hostname, uuid.NewString()[:8])
}

func (s *Server) onBecameLeader(ctx context.Context) {
	s.log.Info("became firehose leader, starting consumer")
	go s.startConsumer()
}

func (s *Server) onLostLeadership(ctx context.Context) {
	s.log.Info("lost firehose leadership, stopping consumer")
	s.stopConsumer()
}

// Starts the firehose consumer in a goroutine. This is invoked when the process
// grabs the leader lock on a background goroutine.
func (s *Server) startConsumer() {
	s.stopConsumer()

	ctx, cancel := context.WithCancel(context.Background())
	s.consumerMu.Lock()
	s.consumerCancel = cancel
	s.consumerMu.Unlock()

	consumer := newFirehoseConsumer(s.log, s.models, s.cfg.FirehoseURL)
	if err := consumer.Run(ctx); err != nil {
		s.log.Error("firehose consumer stopped unexpectedly", "error", err)
	}
}

// Stops the firehose consumer, if running. This is called when the
// process loses leadership.
func (s *Server) stopConsumer() {
	s.consumerMu.Lock()
	defer s.consumerMu.Unlock()

	if s.consumerCancel != nil {
		s.consumerCancel()
		s.consumerCancel = nil
	}
}
