package search

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"strings"

	"github.com/bluesky-social/indigo/atproto/identity"

	"github.com/carlmjohnson/versioninfo"
	"github.com/labstack/echo/v4"
	"github.com/labstack/echo/v4/middleware"
	es "github.com/opensearch-project/opensearch-go/v2"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	slogecho "github.com/samber/slog-echo"
	"go.opentelemetry.io/contrib/instrumentation/github.com/labstack/echo/otelecho"

	_ "net/http/pprof" // For pprof in the metrics server
)

type LastSeq struct {
	ID  uint `gorm:"primarykey"`
	Seq int64
}

type ServerConfig struct {
	Logger            *slog.Logger
	ProfileIndex      string
	PostIndex         string
	AtlantisAddresses []string
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

	serv := Server{
		escli:        escli,
		postIndex:    config.PostIndex,
		profileIndex: config.ProfileIndex,
		dir:          dir,
		logger:       logger,
	}

	return &serv, nil
}

func (s *Server) EnsureIndices(ctx context.Context) error {

	indices := []struct {
		Name       string
		SchemaJSON string
	}{
		{Name: s.postIndex, SchemaJSON: palomarPostSchemaJSON},
		{Name: s.profileIndex, SchemaJSON: palomarProfileSchemaJSON},
	}
	for _, idx := range indices {
		resp, err := s.escli.Indices.Exists([]string{idx.Name})
		if err != nil {
			return err
		}
		defer resp.Body.Close()
		io.ReadAll(resp.Body)
		if resp.IsError() && resp.StatusCode != 404 {
			return fmt.Errorf("failed to check index existence")
		}
		if resp.StatusCode == 404 {
			s.logger.Warn("creating opensearch index", "index", idx.Name)
			if len(idx.SchemaJSON) < 2 {
				return fmt.Errorf("empty schema file (go:embed failed)")
			}
			buf := strings.NewReader(idx.SchemaJSON)
			resp, err := s.escli.Indices.Create(
				idx.Name,
				s.escli.Indices.Create.WithBody(buf))
			if err != nil {
				return err
			}
			defer resp.Body.Close()
			errBytes, err := io.ReadAll(resp.Body)
			if resp.IsError() {
				s.logger.Error("failed to create index", "index", idx.Name, "response", string(errBytes))
				return fmt.Errorf("failed to create index")
			}
			if err != nil {
				return err
			}
		}
	}
	return nil
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
	e.GET("/xrpc/app.bsky.unspecced.searchActorsSkeleton", s.handleSearchActorsSkeleton)
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
