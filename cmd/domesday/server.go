package main

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/bluesky-social/indigo/atproto/identity"
	"github.com/bluesky-social/indigo/atproto/identity/redisdir"

	"github.com/labstack/echo/v4"
	"github.com/labstack/echo/v4/middleware"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	slogecho "github.com/samber/slog-echo"
	"golang.org/x/time/rate"
)

type Server struct {
	dir    identity.Directory
	echo   *echo.Echo
	httpd  *http.Server
	logger *slog.Logger
}

type Config struct {
	Logger       *slog.Logger
	FirehoseHost string
	PLCHost      string
	PLCRateLimit int
	RedisURL     string
	Bind         string
}

func NewServer(config Config) (*Server, error) {
	logger := config.Logger
	if logger == nil {
		logger = slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{
			Level: slog.LevelInfo,
		}))
	}

	baseDir := identity.BaseDirectory{
		PLCURL: config.PLCHost,
		HTTPClient: http.Client{
			Timeout: time.Second * 10, // TODO: config
		},
		PLCLimiter:            rate.NewLimiter(rate.Limit(config.PLCRateLimit), 1),
		TryAuthoritativeDNS:   true,
		SkipDNSDomainSuffixes: []string{".bsky.social", ".staging.bsky.dev"},
	}

	// TODO: config these timeouts
	dir, err := redisdir.NewRedisDirectory(&baseDir, config.RedisURL, time.Hour*24, time.Minute*2, time.Minute*5, 50_000)
	if err != nil {
		return nil, err
	}
	// XXX: dir := identity.NewCacheDirectory(&baseDir, 1_500_000, time.Hour*24, time.Minute*2, time.Minute*5)

	e := echo.New()

	// httpd
	var (
		httpTimeout        = 1 * time.Minute
		httpMaxHeaderBytes = 1 * (1024 * 1024)
	)

	srv := &Server{
		echo: e,
		dir:  identity.DefaultDirectory(),
	}
	srv.httpd = &http.Server{
		Handler:        srv,
		Addr:           config.Bind,
		WriteTimeout:   httpTimeout,
		ReadTimeout:    httpTimeout,
		MaxHeaderBytes: httpMaxHeaderBytes,
	}

	e.HideBanner = true
	e.Use(slogecho.New(logger))
	e.Use(middleware.Recover())
	e.Use(middleware.BodyLimit("4M"))
	e.HTTPErrorHandler = srv.errorHandler
	e.Use(middleware.SecureWithConfig(middleware.SecureConfig{
		ContentTypeNosniff: "nosniff",
		XFrameOptions:      "SAMEORIGIN",
		HSTSMaxAge:         31536000, // 365 days
		// TODO:
		// ContentSecurityPolicy
		// XSSProtection
	}))

	e.GET("/_health", srv.HandleHealthCheck)
	e.GET("/xrpc/com.atproto.identity.resolveHandle", srv.ResolveHandle)
	e.GET("/xrpc/com.atproto.identity.resolveDid", srv.ResolveDid)
	e.GET("/xrpc/com.atproto.identity.resolveIdentity", srv.ResolveIdentity)
	e.POST("/xrpc/com.atproto.identity.refreshIdentity", srv.RefreshIdentity)

	s := &Server{
		dir:    dir,
		logger: logger,
	}

	return s, nil
}

func (srv *Server) ServeHTTP(rw http.ResponseWriter, req *http.Request) {
	srv.echo.ServeHTTP(rw, req)
}

func (srv *Server) RunAPI() error {
	slog.Info("starting server", "bind", srv.httpd.Addr)
	go func() {
		if err := srv.httpd.ListenAndServe(); err != nil {
			if !errors.Is(err, http.ErrServerClosed) {
				slog.Error("HTTP server shutting down unexpectedly", "err", err)
			}
		}
	}()

	// Wait for a signal to exit.
	slog.Info("registering OS exit signal handler")
	quit := make(chan struct{})
	exitSignals := make(chan os.Signal, 1)
	signal.Notify(exitSignals, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		sig := <-exitSignals
		slog.Info("received OS exit signal", "signal", sig)

		// Shut down the HTTP server
		if err := srv.Shutdown(); err != nil {
			slog.Error("HTTP server shutdown error", "err", err)
		}

		// Trigger the return that causes an exit.
		close(quit)
	}()
	<-quit
	slog.Info("graceful shutdown complete")
	return nil
}

func (srv *Server) RunMetrics(listen string) error {
	http.Handle("/metrics", promhttp.Handler())
	return http.ListenAndServe(listen, nil)
}

func (srv *Server) Shutdown() error {
	slog.Info("shutting down")

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	return srv.httpd.Shutdown(ctx)
}
