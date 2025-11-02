package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	comatproto "github.com/bluesky-social/indigo/api/atproto"
	"github.com/bluesky-social/indigo/atproto/identity"
	"github.com/bluesky-social/indigo/cmd/nexus/models"
	"github.com/bluesky-social/indigo/events"
	"gorm.io/driver/postgres"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

type Nexus struct {
	db     *gorm.DB
	logger *slog.Logger

	Dir      identity.Directory
	RelayUrl string

	Server *NexusServer
	Outbox *Outbox

	FirehoseConsumer *FirehoseConsumer
	EventProcessor   *EventProcessor
	Crawler          *Crawler

	FullNetworkMode   bool
	CollectionFilters []string

	config NexusConfig

	claimJobMu   sync.Mutex
	pdsBackoff   map[string]time.Time
	pdsBackoffMu sync.RWMutex
}

type NexusConfig struct {
	DatabaseURL                string
	RelayUrl                   string
	FirehoseParallelism        int
	ResyncParallelism          int
	FirehoseCursorSaveInterval time.Duration
	FullNetworkMode            bool
	SignalCollection           string
	DisableAcks                bool
	WebhookURL                 string
	CollectionFilters          []string // e.g., ["app.bsky.feed.post", "app.bsky.graph.*"]
}

func NewNexus(config NexusConfig) (*Nexus, error) {
	db, err := SetupDatabase(config.DatabaseURL)
	if err != nil {
		return nil, err
	}

	bdir := identity.BaseDirectory{
		TryAuthoritativeDNS:   false,
		SkipDNSDomainSuffixes: []string{".bsky.social"},
	}
	cdir := identity.NewCacheDirectory(&bdir, 2_000_000, time.Hour*24, time.Minute*2, time.Minute*5)

	var outboxMode OutboxMode
	if config.WebhookURL != "" {
		outboxMode = OutboxModeWebhook
	} else if config.DisableAcks {
		outboxMode = OutboxModeFireAndForget
	} else {
		outboxMode = OutboxModeWebsocketAck
	}

	n := &Nexus{
		db:     db,
		logger: slog.Default().With("system", "nexus"),

		Dir:      &cdir,
		RelayUrl: config.RelayUrl,

		Outbox: NewOutbox(db, outboxMode, config.WebhookURL),

		FullNetworkMode:   config.FullNetworkMode,
		CollectionFilters: config.CollectionFilters,

		config: config,

		pdsBackoff: make(map[string]time.Time),
	}

	n.Server = &NexusServer{
		db:     db,
		logger: n.logger.With("component", "server"),
		Outbox: n.Outbox,
	}

	firehoseParallelism := config.FirehoseParallelism
	if firehoseParallelism == 0 {
		firehoseParallelism = 10
	}

	cursorSaveInterval := config.FirehoseCursorSaveInterval
	if cursorSaveInterval == 0 {
		cursorSaveInterval = 5 * time.Second
	}

	n.EventProcessor = &EventProcessor{
		Logger:            n.logger.With("component", "processor"),
		DB:                db,
		Dir:               n.Dir,
		RelayUrl:          config.RelayUrl,
		Outbox:            n.Outbox,
		FullNetworkMode:   config.FullNetworkMode,
		CollectionFilters: config.CollectionFilters,
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
		RelayUrl:    config.RelayUrl,
		Logger:      n.logger.With("component", "firehose"),
		Parallelism: firehoseParallelism,
		Callbacks:   rsc,
		GetCursor:   n.EventProcessor.ReadLastCursor,
	}

	n.Crawler = &Crawler{
		Logger:   n.logger.With("component", "crawler"),
		DB:       db,
		RelayUrl: config.RelayUrl,
	}

	if err := n.resetPartiallyResynced(); err != nil {
		return nil, err
	}

	return n, nil
}

// Run starts internal background workers for resync, cursor saving, and outbox delivery.
func (n *Nexus) Run(ctx context.Context) {
	resyncParallelism := n.config.ResyncParallelism
	if resyncParallelism == 0 {
		resyncParallelism = 5
	}
	for i := 0; i < resyncParallelism; i++ {
		go n.runResyncWorker(ctx, i)
	}

	cursorSaveInterval := n.config.FirehoseCursorSaveInterval
	if cursorSaveInterval == 0 {
		cursorSaveInterval = 5 * time.Second
	}
	go n.EventProcessor.RunCursorSaver(ctx, cursorSaveInterval)

	go n.Outbox.Run(ctx)
}

func (n *Nexus) CloseDb(ctx context.Context) error {
	n.logger.Info("shutting down nexus")

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

func SetupDatabase(dbUrl string) (*gorm.DB, error) {
	// Setup database connection (supports both SQLite and Postgres)
	var dialector gorm.Dialector
	isSqlite := false
	maxOpenConns := 80

	if strings.HasPrefix(dbUrl, "sqlite://") {
		sqlitePath := dbUrl[len("sqlite://"):]
		// Create directory if it doesn't exist
		if err := os.MkdirAll(filepath.Dir(sqlitePath), os.ModePerm); err != nil {
			return nil, err
		}
		// SQLite with pragmas in connection string
		dialector = sqlite.Open(sqlitePath + "?_journal_mode=WAL&_busy_timeout=10000&_synchronous=NORMAL&_temp_store=MEMORY&_cache_size=8000&_wal_autocheckpoint=3000")
		isSqlite = true
		maxOpenConns = 1
	} else if strings.HasPrefix(dbUrl, "postgresql://") || strings.HasPrefix(dbUrl, "postgres://") {
		dialector = postgres.Open(dbUrl)
	} else {
		return nil, fmt.Errorf("unsupported database URL scheme: must start with sqlite://, postgres://, or postgresql://")
	}

	db, err := gorm.Open(dialector, &gorm.Config{
		Logger: logger.Default.LogMode(logger.Silent),
	})
	if err != nil {
		return nil, err
	}

	if isSqlite {
		db.Exec("PRAGMA journal_mode=WAL;")
		db.Exec("PRAGMA synchronous=NORMAL;")
		db.Exec("PRAGMA busy_timeout=10000;")
		db.Exec("PRAGMA temp_store=MEMORY;")
		db.Exec("PRAGMA cache_size=8000;")
		db.Exec("PRAGMA wal_autocheckpoint=3000;")
	} else {
		// Configure connection pool
		sqlDB, err := db.DB()
		if err != nil {
			return nil, err
		}
		sqlDB.SetMaxOpenConns(maxOpenConns)
		sqlDB.SetMaxIdleConns(maxOpenConns)
		sqlDB.SetConnMaxIdleTime(time.Hour)

	}

	if err := db.AutoMigrate(&models.Repo{}, &models.RepoRecord{}, &models.OutboxBuffer{}, &models.ResyncBuffer{}, &models.FirehoseCursor{}, &models.ListReposCursor{}, &models.CollectionCursor{}); err != nil {
		return nil, err
	}

	return db, nil
}
