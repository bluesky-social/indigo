package server

import (
	"context"
	"log/slog"
	"net/http"
	"time"

	_ "net/http/pprof"

	"github.com/bluesky-social/indigo/util/svcutil"

	"github.com/labstack/echo/v4"
	"github.com/labstack/echo/v4/middleware"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

type Config struct {
	Logger         *slog.Logger
	FDBClusterFile string
}

type Server struct {
	cfg Config
	log *slog.Logger

	echo          *echo.Echo
	metricsServer *http.Server
}

func New(config Config) (*Server, error) {
	s := &Server{
		cfg: config,
		log: config.Logger,
	}

	return s, nil
}

func (s *Server) Start(addr string) error {
	e := echo.New()
	e.HideBanner = true
	e.HidePort = true

	e.Use(middleware.CORSWithConfig(middleware.CORSConfig{
		AllowOrigins: []string{"*"},
		AllowHeaders: []string{echo.HeaderOrigin, echo.HeaderContentType, echo.HeaderAccept, echo.HeaderAuthorization},
	}))

	e.Use(svcutil.MetricsMiddleware)
	e.HTTPErrorHandler = s.errorHandler

	// misc. handlers
	e.GET("/", s.handleHome)
	e.GET("/ping", s.handleHealth)
	e.GET("/_health", s.handleHealth)

	// xrpc handlers
	e.GET("/xrpc/_health", s.handleHealth)

	s.echo = e
	return e.Start(addr)
}

func (s *Server) RunMetrics(addr string) error {
	mux := http.NewServeMux()

	mux.Handle("/metrics", promhttp.Handler())
	mux.HandleFunc("/debug/pprof/", http.DefaultServeMux.ServeHTTP)

	s.metricsServer = &http.Server{
		Addr:         addr,
		Handler:      mux,
		ErrorLog:     slog.NewLogLogger(s.log.Handler(), slog.LevelError),
		ReadTimeout:  30 * time.Second,
		WriteTimeout: 30 * time.Second,
	}

	return s.metricsServer.ListenAndServe()
}

func (s *Server) Shutdown(ctx context.Context) error {
	var shutdownErr error

	if s.echo != nil {
		if err := s.echo.Shutdown(ctx); err != nil {
			s.log.Error("error shutting down API server", "error", err)
			shutdownErr = err
		}
	}

	if s.metricsServer != nil {
		s.metricsServer.SetKeepAlivesEnabled(false)
		if err := s.metricsServer.Shutdown(ctx); err != nil {
			s.log.Error("error shutting down metrics server", "error", err)
			if shutdownErr == nil {
				shutdownErr = err
			}
		}
	}

	return shutdownErr
}

func (s *Server) errorHandler(err error, c echo.Context) {
	code := http.StatusInternalServerError
	msg := "internal server error"

	if he, ok := err.(*echo.HTTPError); ok {
		code = he.Code
		if m, ok := he.Message.(string); ok {
			msg = m
		}
	}

	if code >= 500 {
		s.log.Error("handler error", "path", c.Path(), "error", err)
	}

	if !c.Response().Committed {
		if err := c.JSON(code, map[string]any{
			"error": msg,
		}); err != nil {
			s.log.Error("failed to write error response", "error", err)
		}
	}
}

func (s *Server) handleHealth(c echo.Context) error {
	return c.JSON(http.StatusOK, map[string]string{
		"status": "ok",
	})
}
