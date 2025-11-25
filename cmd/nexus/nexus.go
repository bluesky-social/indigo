package main

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/bluesky-social/indigo/atproto/identity"
	"github.com/bluesky-social/indigo/cmd/nexus/models"
	"gorm.io/driver/postgres"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

type Nexus struct {
	db     *gorm.DB
	logger *slog.Logger

	Firehose     *FirehoseProcessor
	Events       *EventManager
	Repos        *RepoManager
	Resyncer     *Resyncer
	ResyncBuffer *ResyncBuffer
	Crawler      *Crawler

	Server *NexusServer
	Outbox *Outbox

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

	logger := slog.Default().With("system", "nexus")

	evtMngr := &EventManager{
		logger:     logger.With("component", "event_manager"),
		db:         db,
		cacheSize:  config.EventCacheSize,
		cache:      make(map[uint]*OutboxEvt),
		pendingIDs: make(chan uint, config.EventCacheSize*10), // give us some buffer room in channel since we can overshoot
	}

	repoMngr := &RepoManager{
		logger: logger.With("component", "server"),
		db:     db,
		IdDir:  &cdir,
		events: evtMngr,
	}

	resyncBuffer := &ResyncBuffer{
		db:     db,
		events: evtMngr,
	}

	resyncer := &Resyncer{
		logger:            logger.With("component", "resyncer"),
		db:                db,
		events:            evtMngr,
		repos:             repoMngr,
		resyncBuffer:      resyncBuffer,
		repoFetchTimeout:  config.RepoFetchTimeout,
		collectionFilters: config.CollectionFilters,
		parallelism:       config.ResyncParallelism,
		pdsBackoff:        make(map[string]time.Time),
	}

	firehose := &FirehoseProcessor{
		logger:             logger.With("component", "firehose"),
		db:                 db,
		events:             evtMngr,
		repos:              repoMngr,
		resyncBuffer:       resyncBuffer,
		relayUrl:           config.RelayUrl,
		fullNetworkMode:    config.FullNetworkMode,
		signalCollection:   config.SignalCollection,
		collectionFilters:  config.CollectionFilters,
		parallelism:        config.FirehoseParallelism,
		cursorSaveInterval: config.FirehoseCursorSaveInterval,
	}

	crawler := &Crawler{
		logger:   logger.With("component", "crawler"),
		db:       db,
		relayUrl: config.RelayUrl,
	}

	outbox := &Outbox{
		logger:      logger.With("component", "outbox"),
		mode:        parseOutboxMode(config.WebhookURL, config.DisableAcks),
		parallelism: config.OutboxParallelism,
		webhookURL:  config.WebhookURL,
		httpClient: &http.Client{
			Timeout: 30 * time.Second,
		},
		events:   evtMngr,
		acks:     make(chan uint, config.OutboxParallelism*10000),
		outgoing: make(chan *OutboxEvt, config.OutboxParallelism*10000),
	}

	server := &NexusServer{
		logger: logger.With("component", "server"),
		db:     db,
		outbox: outbox,
	}

	n := &Nexus{
		db:     db,
		logger: slog.Default().With("system", "nexus"),

		Firehose:     firehose,
		Events:       evtMngr,
		Repos:        repoMngr,
		Resyncer:     resyncer,
		ResyncBuffer: resyncBuffer,
		Crawler:      crawler,
		Server:       server,
		Outbox:       outbox,

		config: config,
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
