package main

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
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
	EventProcessor   *FirehoseProcessor
	Events           *EventManager
	Repos            *RepoManager
	Resyncer         *Resyncer
	ResyncBuffer     *ResyncBuffer
	Crawler          *Crawler

	FullNetworkMode   bool
	CollectionFilters []string

	config NexusConfig
}

type NexusConfig struct {
	DatabaseURL                string
	DBMaxConns                 int
	RelayUrl                   string
	FirehoseParallelism        int
	ResyncParallelism          int
	OutboxParallelism          int
	FirehoseCursorSaveInterval time.Duration
	RepoFetchTimeout           time.Duration
	IdentityCacheSize          int
	EventCacheSize             int
	FullNetworkMode            bool
	SignalCollection           string
	DisableAcks                bool
	WebhookURL                 string
	CollectionFilters          []string // e.g., ["app.bsky.feed.post", "app.bsky.graph.*"]
	OutboxOnly                 bool
}

func NewNexus(config NexusConfig) (*Nexus, error) {
	db, err := SetupDatabase(config.DatabaseURL, config.DBMaxConns)
	if err != nil {
		return nil, err
	}

	bdir := identity.BaseDirectory{
		TryAuthoritativeDNS:   false,
		SkipDNSDomainSuffixes: []string{".bsky.social"},
	}
	cdir := identity.NewCacheDirectory(&bdir, config.IdentityCacheSize, time.Hour*24, time.Minute*2, time.Minute*5)

	evtManager := NewEventManager(db, config.EventCacheSize)

	var outboxMode OutboxMode
	if config.WebhookURL != "" {
		outboxMode = OutboxModeWebhook
	} else if config.DisableAcks {
		outboxMode = OutboxModeFireAndForget
	} else {
		outboxMode = OutboxModeWebsocketAck
	}
	outbox := NewOutbox(evtManager, outboxMode, config.WebhookURL, config.OutboxParallelism)

	n := &Nexus{
		db:     db,
		logger: slog.Default().With("system", "nexus"),

		Dir:      &cdir,
		RelayUrl: config.RelayUrl,

		Outbox: outbox,
		Events: evtManager,
		ResyncBuffer: &ResyncBuffer{
			db:     db,
			events: evtManager,
		},

		FullNetworkMode:   config.FullNetworkMode,
		CollectionFilters: config.CollectionFilters,

		config: config,
	}

	n.Repos = &RepoManager{
		logger: n.logger.With("component", "server"),
		db:     db,
		IdDir:  n.Dir,
		events: n.Events,
	}

	n.Resyncer = &Resyncer{
		logger:            n.logger.With("component", "resyncer"),
		db:                db,
		events:            n.Events,
		repos:             n.Repos,
		resyncBuffer:      n.ResyncBuffer,
		repoFetchTimeout:  n.config.RepoFetchTimeout,
		collectionFilters: n.config.CollectionFilters,
		parallelism:       n.config.ResyncParallelism,
		pdsBackoff:        make(map[string]time.Time),
	}

	n.EventProcessor = &FirehoseProcessor{
		Logger:            n.logger.With("component", "processor"),
		DB:                db,
		RelayUrl:          config.RelayUrl,
		Events:            evtManager,
		FullNetworkMode:   config.FullNetworkMode,
		SignalCollection:  config.SignalCollection,
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
		Parallelism: config.FirehoseParallelism,
		Callbacks:   rsc,
		GetCursor:   n.EventProcessor.ReadLastCursor,
	}

	n.Crawler = &Crawler{
		Logger:   n.logger.With("component", "crawler"),
		DB:       db,
		RelayUrl: config.RelayUrl,
	}

	n.Server = &NexusServer{
		logger: n.logger.With("component", "server"),
		db:     db,
		Outbox: n.Outbox,
	}

	if err := n.Resyncer.resetPartiallyResynced(); err != nil {
		return nil, err
	}

	return n, nil
}

// Run starts internal background workers for resync, cursor saving, and outbox delivery.
func (n *Nexus) Run(ctx context.Context) {
	go n.Events.LoadEvents(ctx)

	if !n.config.OutboxOnly {
		go n.Resyncer.run(ctx)
		go n.EventProcessor.RunCursorSaver(ctx, n.config.FirehoseCursorSaveInterval)
	}

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

func SetupDatabase(dbUrl string, maxConns int) (*gorm.DB, error) {
	// Setup database connection (supports both SQLite and Postgres)
	var dialector gorm.Dialector
	isSqlite := false

	if strings.HasPrefix(dbUrl, "sqlite://") {
		sqlitePath := dbUrl[len("sqlite://"):]
		dialector = sqlite.Open(sqlitePath)
		isSqlite = true
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
		sqlDB.SetMaxOpenConns(maxConns)
		sqlDB.SetMaxIdleConns(maxConns)
		sqlDB.SetConnMaxIdleTime(time.Hour)

	}

	if err := db.AutoMigrate(&models.Repo{}, &models.RepoRecord{}, &models.OutboxBuffer{}, &models.ResyncBuffer{}, &models.FirehoseCursor{}, &models.ListReposCursor{}, &models.CollectionCursor{}); err != nil {
		return nil, err
	}

	return db, nil
}
