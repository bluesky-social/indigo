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

	FullNetworkMode bool
	RelayHost       string
}

type NexusConfig struct {
	DBPath                     string
	RelayHost                  string
	FirehoseParallelism        int
	FirehoseCursorSaveInterval time.Duration
	FullNetworkMode            bool
}

func NewNexus(config NexusConfig) (*Nexus, error) {
	db, err := gorm.Open(sqlite.Open(config.DBPath+"?_journal_mode=WAL"), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Silent),
	})
	if err != nil {
		return nil, err
	}

	if err := db.AutoMigrate(&models.Repo{}, &models.RepoRecord{}, &models.OutboxBuffer{}, &models.ResyncBuffer{}, &models.FirehoseCursor{}, &models.ListReposCursor{}); err != nil {
		return nil, err
	}

	e := echo.New()
	e.HideBanner = true

	bdir := identity.BaseDirectory{
		TryAuthoritativeDNS:   false,
		SkipDNSDomainSuffixes: []string{".bsky.social"},
	}
	cdir := identity.NewCacheDirectory(&bdir, 2_000_000, time.Hour*24, time.Minute*2, time.Minute*5)

	n := &Nexus{
		db:     db,
		echo:   e,
		logger: slog.Default().With("system", "nexus"),

		Dir: &cdir,

		outbox: NewOutbox(db),

		FullNetworkMode: config.FullNetworkMode,
		RelayHost:       config.RelayHost,
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
		Logger:          n.logger.With("component", "processor"),
		DB:              db,
		Dir:             n.Dir,
		RelayHost:       config.RelayHost,
		Outbox:          n.outbox,
		FullNetworkMode: config.FullNetworkMode,
	}

	rsc := &events.RepoStreamCallbacks{
		RepoCommit: func(evt *comatproto.SyncSubscribeRepos_Commit) error {
			return n.EventProcessor.ProcessCommit(context.Background(), evt)
		},
		RepoSync: func(evt *comatproto.SyncSubscribeRepos_Sync) error {
			return n.EventProcessor.ProcessSync(context.Background(), evt)
		},
		RepoIdentity: func(evt *comatproto.SyncSubscribeRepos_Identity) error {
			return n.EventProcessor.ProcessIdentity(context.Background(), evt)
		},
		RepoAccount: func(evt *comatproto.SyncSubscribeRepos_Account) error {
			return n.EventProcessor.ProcessAccount(context.Background(), evt)
		},
	}

	n.FirehoseConsumer = &FirehoseConsumer{
		RelayHost:   config.RelayHost,
		Logger:      n.logger.With("component", "firehose"),
		Parallelism: parallelism,
		Callbacks:   rsc,
		GetCursor:   n.EventProcessor.ReadLastCursor,
	}

	// crash recovery: reset any partially repos
	if err := n.resetPartiallyResynced(); err != nil {
		return nil, err
	}

	if config.FullNetworkMode {
		go func() {
			if err := n.EnumerateNetwork(context.Background()); err != nil {
				n.logger.Error("network enumeration failed", "error", err)
			}
		}()
	}

	for i := 0; i < 20; i++ {
		go n.runResyncWorker(context.Background(), i)
	}

	go n.EventProcessor.RunCursorSaver(context.Background(), cursorSaveInterval)
	go n.outbox.Run(context.Background())

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
