package search

import (
	"context"
	"log/slog"
	"net/http"
	"os"

	"github.com/bluesky-social/indigo/atproto/identity"

	"github.com/carlmjohnson/versioninfo"
	"github.com/labstack/echo/v4"
	"github.com/labstack/echo/v4/middleware"
	es "github.com/opensearch-project/opensearch-go/v2"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	slogecho "github.com/samber/slog-echo"
	"go.opentelemetry.io/contrib/instrumentation/github.com/labstack/echo/otelecho"
)

type LastSeq struct {
	ID  uint `gorm:"primarykey"`
	Seq int64
}

type ServerConfig struct {
	Logger       *slog.Logger
	ProfileIndex string
	PostIndex    string
}

type Server struct {
	escli        *es.Client
	postIndex    string
	profileIndex string
	dir          identity.Directory
	echo         *echo.Echo
	logger       *slog.Logger

	Indexer *Indexer
}

func NewServer(escli *es.Client, dir identity.Directory, config ServerConfig) (*Server, error) {
	logger := config.Logger
	if logger == nil {
		logger = slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{
			Level: slog.LevelInfo,
		}))
	}

	return &Server{
		escli:        escli,
		postIndex:    config.PostIndex,
		profileIndex: config.ProfileIndex,
		dir:          dir,
		logger:       logger,
	}, nil
}

type HealthStatus struct {
	Service string `json:"service,const=palomar"`
	Status  string `json:"status"`
	Version string `json:"version"`
	Message string `json:"msg,omitempty"`
}

func (a *Server) handleHealthCheck(c echo.Context) error {
	if a.Indexer != nil {
		if err := a.Indexer.db.Exec("SELECT 1").Error; err != nil {
			a.logger.Error("healthcheck can't connect to database", "err", err)
			return c.JSON(500, HealthStatus{Status: "error", Version: versioninfo.Short(), Message: "can't connect to database"})
		}
	}
	return c.JSON(200, HealthStatus{Status: "ok", Version: versioninfo.Short()})
}

func (s *Server) RunAPI(listen string) error {
	s.logger.Info("Configuring HTTP server")
	e := echo.New()
	e.HideBanner = true
	e.Use(slogecho.New(s.logger))
	e.Use(middleware.Recover())
	e.Use(MetricsMiddleware)
	e.Use(middleware.BodyLimit("64M"))
	e.Use(otelecho.Middleware("palomar"))

	e.HTTPErrorHandler = func(err error, ctx echo.Context) {
		code := 500
		if he, ok := err.(*echo.HTTPError); ok {
			code = he.Code
		}
		s.logger.Warn("HTTP request error", "statusCode", code, "path", ctx.Path(), "err", err)
		ctx.Response().WriteHeader(code)
	}

	e.Use(middleware.CORS())
	e.GET("/", s.handleHealthCheck)
	e.GET("/_health", s.handleHealthCheck)
	e.GET("/metrics", echo.WrapHandler(promhttp.Handler()))
	e.GET("/xrpc/app.bsky.unspecced.searchPostsSkeleton", s.handleSearchPostsSkeleton)
	e.GET("/xrpc/app.bsky.unspecced.structuredSearchPostsSkeleton", s.handleStructuredSearchPostsSkeleton)
	e.GET("/xrpc/app.bsky.unspecced.searchActorsSkeleton", s.handleSearchActorsSkeleton)
	e.GET("/xrpc/app.bsky.unspecced.structuredSearchActorsSkeleton", s.handleStructuredSearchActorsSkeleton)
	s.echo = e

	s.logger.Info("starting search API daemon", "bind", listen)
	return s.echo.Start(listen)
}

func (s *Server) RunMetrics(listen string) error {
	http.Handle("/metrics", promhttp.Handler())
	return http.ListenAndServe(listen, nil)
}

func (s *Server) Shutdown(ctx context.Context) error {
	return s.echo.Shutdown(ctx)
}
