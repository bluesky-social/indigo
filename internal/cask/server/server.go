package server

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"sync"

	_ "net/http/pprof"

	"github.com/bluesky-social/indigo/internal/cask/firehose"
	"github.com/bluesky-social/indigo/internal/cask/models"
	"github.com/bluesky-social/indigo/pkg/foundation"
	"github.com/bluesky-social/indigo/pkg/foundation/leader"
	"github.com/bluesky-social/indigo/pkg/metrics"
	"github.com/bluesky-social/indigo/util/svcutil"
	"github.com/bluesky-social/indigo/xrpc"
	"github.com/google/uuid"
	"github.com/labstack/echo/v4"
	"go.opentelemetry.io/otel"
	"golang.org/x/sync/errgroup"
)

type Config struct {
	Logger         *slog.Logger
	FDBClusterFile string
	FirehoseURL    string
}

type Server struct {
	cfg Config
	log *slog.Logger

	echo          *echo.Echo
	metricsServer *http.Server

	db             *foundation.DB
	models         *models.Models
	leaderElection *leader.LeaderElection

	consumerMu     sync.Mutex
	consumerCancel context.CancelFunc
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

	s := &Server{
		cfg:    config,
		log:    config.Logger,
		db:     db,
		models: m,
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
	e := echo.New()
	e.HideBanner = true
	e.HidePort = true

	e.Use(svcutil.MetricsMiddleware)
	e.HTTPErrorHandler = s.errorHandler

	// misc. handlers
	e.GET("/", s.handleHome)
	e.GET("/ping", s.handleHealth)

	// xrpc handlers
	e.GET("/xrpc/_health", s.handleHealth)

	s.echo = e

	go func() {
		if err := s.leaderElection.Run(ctx); err != nil && ctx.Err() == nil {
			s.log.Error("leader election stopped unexpectedly", "error", err)
		}
	}()

	return e.Start(addr)
}

// Gracefully stops the server process. If we're the leaseholder of the firehose consumer
// lock, we stop the consumer and release the lease. Then, we stop the
func (s *Server) Shutdown(ctx context.Context) error {
	s.stopConsumer()
	s.leaderElection.Stop()

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

	if code >= 500 {
		s.log.Error("handler error", "path", c.Path(), "error", err)
	}

	if !c.Response().Committed {
		if err := c.JSON(code, xrpc.XRPCError{ErrStr: errStr, Message: msg}); err != nil {
			s.log.Error("failed to write error response", "error", err)
		}
	}
}

func (s *Server) handleHealth(c echo.Context) error {
	return c.JSON(http.StatusOK, map[string]string{
		"status": "ok",
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
	s.consumerCancel()

	ctx, cancel := context.WithCancel(context.Background())
	s.consumerMu.Lock()
	s.consumerCancel = cancel
	s.consumerMu.Unlock()

	consumer := firehose.NewConsumer(s.log, s.models, s.cfg.FirehoseURL)
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
