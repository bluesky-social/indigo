package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/bluesky-social/indigo/atproto/identity"
	//"github.com/bluesky-social/indigo/atproto/identity/redisdir"

	"github.com/labstack/echo/v4"
	"github.com/labstack/echo/v4/middleware"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/redis/go-redis/v9"
	slogecho "github.com/samber/slog-echo"
	"golang.org/x/time/rate"
)

type Server struct {
	dir    *RedisResolver
	echo   *echo.Echo
	httpd  *http.Server
	logger *slog.Logger

	// this redis client is used to store firehose offset
	redisClient *redis.Client

	// lastSeq is the most recent event sequence number we've received and begun to handle.
	// This number is periodically persisted to redis, if redis is present.
	// The value is best-effort (the stream handling itself is concurrent, so event numbers may not be monotonic),
	// but nonetheless, you must use atomics when updating or reading this (to avoid data races).
	lastSeq int64
}

type Config struct {
	Logger         *slog.Logger
	PLCHost        string
	PLCRateLimit   int
	RedisURL       string
	Bind           string
	DisableRefresh bool
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
			Timeout: time.Second * 10,
			Transport: &http.Transport{
				// would want this around 100ms for services doing lots of handle resolution (to reduce number of idle connections). Impacts PLC connections as well, but not too bad.
				IdleConnTimeout: time.Millisecond * 100,
				MaxIdleConns:    1000,
			},
		},
		Resolver: net.Resolver{
			Dial: func(ctx context.Context, network, address string) (net.Conn, error) {
				d := net.Dialer{Timeout: time.Second * 3}
				return d.DialContext(ctx, network, address)
			},
		},
		PLCLimiter:            rate.NewLimiter(rate.Limit(config.PLCRateLimit), 1),
		TryAuthoritativeDNS:   true,
		SkipDNSDomainSuffixes: []string{".bsky.social", ".staging.bsky.dev"},
		// TODO: UserAgent: "domesday",
	}

	// TODO: config these timeouts
	redisDir, err := NewRedisResolver(&baseDir, config.RedisURL, time.Hour*24, time.Minute*2, time.Minute*5, 50_000)
	if err != nil {
		return nil, err
	}

	// configure redis client (for firehose consumer)
	redisOpt, err := redis.ParseURL(config.RedisURL)
	if err != nil {
		return nil, fmt.Errorf("parsing redis URL: %v", err)
	}
	redisClient := redis.NewClient(redisOpt)
	// check redis connection
	_, err = redisClient.Ping(context.Background()).Result()
	if err != nil {
		return nil, fmt.Errorf("redis ping failed: %v", err)
	}

	e := echo.New()

	// httpd
	var (
		httpTimeout        = 1 * time.Minute
		httpMaxHeaderBytes = 1 * (1024 * 1024)
	)

	srv := &Server{
		echo:        e,
		dir:         redisDir,
		logger:      logger,
		redisClient: redisClient,
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
	if !config.DisableRefresh {
		e.POST("/xrpc/com.atproto.identity.refreshIdentity", srv.RefreshIdentity)
	}

	return srv, nil
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

type GenericError struct {
	Error   string `json:"error"`
	Message string `json:"message"`
}

func (srv *Server) errorHandler(err error, c echo.Context) {
	code := http.StatusInternalServerError
	var errorMessage string
	if he, ok := err.(*echo.HTTPError); ok {
		code = he.Code
		errorMessage = fmt.Sprintf("%s", he.Message)
	}
	if code >= 500 {
		slog.Warn("domesday-http-internal-error", "err", err)
	}
	if !c.Response().Committed {
		c.JSON(code, GenericError{Error: "InternalError", Message: errorMessage})
	}
}
