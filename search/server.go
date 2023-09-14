package search

import (
	"context"
	"fmt"
	"io/ioutil"
	"log/slog"
	"os"
	"strings"
	_ "embed"

	"github.com/bluesky-social/indigo/atproto/identity"
	"github.com/bluesky-social/indigo/backfill"
	"github.com/bluesky-social/indigo/util/version"
	"github.com/bluesky-social/indigo/xrpc"

	"github.com/labstack/echo-contrib/echoprometheus"
	"github.com/labstack/echo/v4"
	"github.com/labstack/echo/v4/middleware"
	es "github.com/opensearch-project/opensearch-go/v2"
	slogecho "github.com/samber/slog-echo"
	gorm "gorm.io/gorm"
)

type Server struct {
	escli        *es.Client
	postIndex    string
	profileIndex string
	db           *gorm.DB
	bgshost      string
	bgsxrpc      *xrpc.Client
	dir          identity.Directory
	echo         *echo.Echo
	logger       *slog.Logger

	bfs *backfill.Gormstore
	bf  *backfill.Backfiller
}

type LastSeq struct {
	ID  uint `gorm:"primarykey"`
	Seq int64
}

type Config struct {
	BGSHost      string
	ProfileIndex string
	PostIndex    string
	Logger       *slog.Logger
}

func NewServer(db *gorm.DB, escli *es.Client, dir identity.Directory, config Config) (*Server, error) {

	logger := config.Logger
	if logger == nil {
		logger = slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{
			Level: slog.LevelInfo,
		}))
	}

	logger.Info("running database migrations")
	db.AutoMigrate(&LastSeq{})
	db.AutoMigrate(&backfill.GormDBJob{})

	bgsws := config.BGSHost
	if !strings.HasPrefix(bgsws, "ws") {
		return nil, fmt.Errorf("specified bgs host must include 'ws://' or 'wss://'")
	}

	bgshttp := strings.Replace(bgsws, "ws", "http", 1)
	bgsxrpc := &xrpc.Client{
		Host: bgshttp,
	}

	s := &Server{
		escli:        escli,
		profileIndex: config.ProfileIndex,
		postIndex:    config.PostIndex,
		db:           db,
		bgshost:      config.BGSHost, // NOTE: the original URL, not 'bgshttp'
		bgsxrpc:      bgsxrpc,
		dir:          dir,
		logger:       logger,
	}

	bfstore := backfill.NewGormstore(db)
	opts := backfill.DefaultBackfillOptions()
	opts.ParallelRecordCreates = 20
	opts.SyncRequestsPerSecond = 8
	opts.NSIDFilter = "app.bsky."
	bf := backfill.NewBackfiller(
		"search",
		bfstore,
		s.handleCreateOrUpdate,
		s.handleCreateOrUpdate,
		s.handleDelete,
		opts,
	)

	s.bfs = bfstore
	s.bf = bf

	return s, nil
}

//go:embed post_schema.json
var palomarPostSchemaJSON string

//go:embed profile_schema.json
var palomarProfileSchemaJSON string

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
		ioutil.ReadAll(resp.Body)
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
			ioutil.ReadAll(resp.Body)
			if resp.IsError() {
				return fmt.Errorf("failed to create index")
			}
		}
	}
	return nil
}

type HealthStatus struct {
	Status  string `json:"status"`
	Version string `json:"version"`
	Message string `json:"msg,omitempty"`
}

func (s *Server) handleHealthCheck(c echo.Context) error {
	if err := s.db.Exec("SELECT 1").Error; err != nil {
		s.logger.Error("healthcheck can't connect to database", "err", err)
		return c.JSON(500, HealthStatus{Status: "error", Version: version.Version, Message: "can't connect to database"})
	} else {
		return c.JSON(200, HealthStatus{Status: "ok", Version: version.Version})
	}
}

func (s *Server) RunAPI(listen string) error {

	s.logger.Info("Configuring HTTP server")
	e := echo.New()
	e.HideBanner = true
	e.Use(slogecho.New(s.logger))
	e.Use(middleware.Recover())
	e.Use(echoprometheus.NewMiddleware("palomar"))
	e.Use(middleware.BodyLimit("64M"))

	e.HTTPErrorHandler = func(err error, ctx echo.Context) {
		code := 500
		if he, ok := err.(*echo.HTTPError); ok {
			code = he.Code
		}
		s.logger.Warn("HTTP request error", "statusCode", code, "path", ctx.Path(), "err", err)
		ctx.Response().WriteHeader(code)
	}

	e.Use(middleware.CORS())
	e.GET("/_health", s.handleHealthCheck)
	e.GET("/metrics", echoprometheus.NewHandler())
	e.GET("/xrpc/app.bsky.unspecced.searchPostsSkeleton", s.handleSearchPostsSkeleton)
	e.GET("/xrpc/app.bsky.unspecced.searchActorsSkeleton", s.handleSearchActorsSkeleton)
	s.echo = e

	s.logger.Info("starting search API daemon", "bind", listen)
	return s.echo.Start(listen)
}

func (s *Server) Shutdown(ctx context.Context) error {
	return s.echo.Shutdown(ctx)
}
