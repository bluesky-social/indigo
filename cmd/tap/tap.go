package main

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/bluesky-social/indigo/atproto/identity"
	"github.com/bluesky-social/indigo/cmd/tap/models"
	"github.com/puzpuzpuz/xsync/v4"
	"gorm.io/driver/postgres"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

type Tap struct {
	db     *gorm.DB
	logger *slog.Logger

	Firehose *FirehoseProcessor
	Events   *EventManager
	Repos    *RepoManager
	Resyncer *Resyncer
	Crawler  *Crawler

	Server *TapServer
	Outbox *Outbox

	config TapConfig
}

type TapConfig struct {
	DatabaseURL                string
	DBMaxConns                 int
	PLCURL                     string
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
	AdminPassword              string
	RetryTimeout               time.Duration
}

func NewTap(config TapConfig) (*Tap, error) {
	db, err := SetupDatabase(config.DatabaseURL, config.DBMaxConns)
	if err != nil {
		return nil, err
	}

	bdir := identity.BaseDirectory{
		PLCURL:                config.PLCURL,
		TryAuthoritativeDNS:   false,
		SkipDNSDomainSuffixes: []string{".bsky.social"},
	}
	cdir := identity.NewCacheDirectory(&bdir, config.IdentityCacheSize, time.Hour*24, time.Minute*2, time.Minute*5)

	logger := slog.Default().With("system", "tap")

	evtMngr := &EventManager{
		logger:     logger.With("component", "event_manager"),
		db:         db,
		cacheSize:  config.EventCacheSize,
		cache:      make(map[uint]*OutboxEvt),
		pendingIDs: make(chan uint, config.EventCacheSize*2), // give us some buffer room in channel since we can overshoot
	}

	repoMngr := &RepoManager{
		logger: logger.With("component", "server"),
		db:     db,
		IdDir:  &cdir,
		events: evtMngr,
	}

	resyncer := &Resyncer{
		logger:            logger.With("component", "resyncer"),
		db:                db,
		events:            evtMngr,
		repos:             repoMngr,
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
		relayUrl:           config.RelayUrl,
		fullNetworkMode:    config.FullNetworkMode,
		signalCollection:   config.SignalCollection,
		collectionFilters:  config.CollectionFilters,
		parallelism:        config.FirehoseParallelism,
		cursorSaveInterval: config.FirehoseCursorSaveInterval,
	}

	crawler := &Crawler{
		logger:           logger.With("component", "crawler"),
		db:               db,
		FullNetworkMode:  config.FullNetworkMode,
		RelayUrl:         config.RelayUrl,
		SignalCollection: config.SignalCollection,
	}

	outbox := &Outbox{
		logger:       logger.With("component", "outbox"),
		mode:         parseOutboxMode(config.WebhookURL, config.DisableAcks),
		parallelism:  config.OutboxParallelism,
		retryTimeout: config.RetryTimeout,
		webhook: &WebhookClient{
			logger:        logger.With("component", "webhook_client"),
			webhookURL:    config.WebhookURL,
			adminPassword: config.AdminPassword,
			httpClient: &http.Client{
				Timeout: 30 * time.Second,
			},
		},
		events:     evtMngr,
		didWorkers: xsync.NewMap[string, *DIDWorker](),
		acks:       make(chan uint, config.OutboxParallelism*10000),
		outgoing:   make(chan *OutboxEvt, config.OutboxParallelism*10000),
	}

	server := &TapServer{
		logger:        logger.With("component", "server"),
		db:            db,
		outbox:        outbox,
		adminPassword: config.AdminPassword,
		idDir:         repoMngr.IdDir,
		firehose:      firehose,
		crawler:       crawler,
	}

	t := &Tap{
		db:     db,
		logger: slog.Default().With("system", "tap"),

		Firehose: firehose,
		Events:   evtMngr,
		Repos:    repoMngr,
		Resyncer: resyncer,
		Crawler:  crawler,
		Server:   server,
		Outbox:   outbox,

		config: config,
	}

	if err := t.Resyncer.resetPartiallyResynced(context.Background()); err != nil {
		return nil, err
	}

	return t, nil
}

// Run starts internal background workers for resync, cursor saving, and outbox delivery.
func (t *Tap) Run(ctx context.Context) {
	go t.Events.LoadEvents(ctx)

	if !t.config.OutboxOnly {
		go t.Resyncer.run(ctx)
	}

	go t.Outbox.Run(ctx)
}

func (t *Tap) CloseDb(ctx context.Context) error {
	t.logger.Info("shutting down tap")

	sqlDB, err := t.db.DB()
	if err != nil {
		t.logger.Error("error getting sql db", "error", err)
		return err
	}

	if err := sqlDB.Close(); err != nil {
		t.logger.Error("error closing sqlite db", "error", err)
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
