package main

import (
	"context"
	"log/slog"
	"time"

	comatproto "github.com/bluesky-social/indigo/api/atproto"
	"github.com/bluesky-social/indigo/atproto/identity"
	"github.com/bluesky-social/indigo/events"
	"github.com/bluesky-social/indigo/nexus/models"
	"github.com/labstack/echo/v4"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

type Nexus struct {
	db     *gorm.DB
	echo   *echo.Echo
	logger *slog.Logger

	Dir identity.Directory

	outbox *Outbox

	FirehoseConsumer *FirehoseConsumer
	EventProcessor   *EventProcessor
}

type NexusConfig struct {
	DBPath                     string
	RelayHost                  string
	FirehoseParallelism        int
	FirehoseCursorSaveInterval time.Duration
}

func NewNexus(config NexusConfig) (*Nexus, error) {
	db, err := gorm.Open(sqlite.Open(config.DBPath+"?_journal_mode=WAL"), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Silent),
	})
	if err != nil {
		return nil, err
	}

	if err := db.AutoMigrate(&models.Did{}, &models.RepoRecord{}, &models.OutboxBuffer{}, &models.BackfillBuffer{}, &models.Cursor{}); err != nil {
		return nil, err
	}

	e := echo.New()
	e.HideBanner = true

	bdir := identity.BaseDirectory{
		SkipHandleVerification: true,
		TryAuthoritativeDNS:    false,
		SkipDNSDomainSuffixes:  []string{".bsky.social"},
	}
	cdir := identity.NewCacheDirectory(&bdir, 1_000_000, time.Hour*24, time.Minute*2, time.Minute*5)

	n := &Nexus{
		db:     db,
		echo:   e,
		logger: slog.Default().With("system", "nexus"),

		Dir: &cdir,

		outbox: NewOutbox(db),
	}

	parallelism := config.FirehoseParallelism
	if parallelism == 0 {
		parallelism = 10
	}

	cursorSaveInterval := config.FirehoseCursorSaveInterval
	if cursorSaveInterval == 0 {
		cursorSaveInterval = 5 * time.Second
	}

	n.EventProcessor = &EventProcessor{
		Logger:    n.logger.With("component", "processor"),
		DB:        db,
		Dir:       n.Dir,
		RelayHost: config.RelayHost,
		Outbox:    n.outbox,
	}

	cursor, err := n.EventProcessor.ReadLastCursor(context.Background(), config.RelayHost)
	if err != nil {
		return nil, err
	}

	rsc := &events.RepoStreamCallbacks{
		RepoCommit: func(evt *comatproto.SyncSubscribeRepos_Commit) error {
			return n.EventProcessor.ProcessCommit(context.Background(), evt)
		},
	}

	n.FirehoseConsumer = &FirehoseConsumer{
		RelayHost:   config.RelayHost,
		Cursor:      cursor,
		Logger:      n.logger.With("component", "firehose"),
		Parallelism: parallelism,
		Callbacks:   rsc,
	}

	// crash recovery: reset any backfilling repos to pending on service startup
	if err := n.resetBackfillingToPending(); err != nil {
		return nil, err
	}

	for i := 0; i < 50; i++ {
		go n.runBackfillWorker(context.Background(), i)
	}

	go n.EventProcessor.RunCursorSaver(context.Background(), cursorSaveInterval)

	n.registerRoutes()

	return n, nil
}

func (n *Nexus) Start(ctx context.Context, addr string) error {
	n.logger.Info("starting nexus server", "addr", addr)
	return n.echo.Start(addr)
}

func (n *Nexus) Shutdown(ctx context.Context) error {
	n.logger.Info("shutting down nexus server")
	if err := n.echo.Shutdown(ctx); err != nil {
		n.logger.Error("error shutting down echo", "error", err)
	}

	sqlDB, err := n.db.DB()
	if err != nil {
		n.logger.Error("error getting sql db", "error", err)
		return err
	}

	if err := sqlDB.Close(); err != nil {
		n.logger.Error("error closing sqlite db", "error", err)
		return err
	}

	return nil
}
