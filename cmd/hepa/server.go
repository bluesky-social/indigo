package main

import (
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"strings"

	"github.com/bluesky-social/indigo/atproto/identity"
	"github.com/bluesky-social/indigo/automod"
	"github.com/bluesky-social/indigo/backfill"
	"github.com/bluesky-social/indigo/xrpc"

	"github.com/prometheus/client_golang/prometheus/promhttp"
	gorm "gorm.io/gorm"
)

type Server struct {
	db           *gorm.DB
	bgshost      string
	bgsxrpc      *xrpc.Client
	dir          identity.Directory
	logger       *slog.Logger
	engine       *automod.Engine
	skipBackfill bool

	bfs *backfill.Gormstore
	bf  *backfill.Backfiller
}

type LastSeq struct {
	ID  uint `gorm:"primarykey"`
	Seq int64
}

type Config struct {
	BGSHost             string
	Logger              *slog.Logger
	BGSSyncRateLimit    int
	MaxEventConcurrency int
}

func NewServer(db *gorm.DB, dir identity.Directory, config Config) (*Server, error) {
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

	engine := automod.Engine{
		Directory: dir,
	}

	s := &Server{
		db:           db,
		bgshost:      config.BGSHost, // NOTE: the original URL, not 'bgshttp'
		bgsxrpc:      bgsxrpc,
		dir:          dir,
		logger:       logger,
		engine:       &engine,
		skipBackfill: true,
	}

	bfstore := backfill.NewGormstore(db)
	opts := backfill.DefaultBackfillOptions()
	if config.BGSSyncRateLimit > 0 {
		opts.SyncRequestsPerSecond = config.BGSSyncRateLimit
		opts.ParallelBackfills = 2 * config.BGSSyncRateLimit
	} else {
		opts.SyncRequestsPerSecond = 8
	}
	opts.CheckoutPath = fmt.Sprintf("%s/xrpc/com.atproto.sync.getRepo", bgshttp)
	if config.MaxEventConcurrency > 0 {
		opts.ParallelRecordCreates = config.MaxEventConcurrency
	} else {
		opts.ParallelRecordCreates = 20
	}
	opts.NSIDFilter = "app.bsky."
	bf := backfill.NewBackfiller(
		"hepa",
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

func (s *Server) RunMetrics(listen string) error {
	http.Handle("/metrics", promhttp.Handler())
	return http.ListenAndServe(listen, nil)
}
