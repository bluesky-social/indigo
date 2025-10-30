package main

import (
	"context"
	"log/slog"
	"sync"
	"time"

	comatproto "github.com/bluesky-social/indigo/api/atproto"
	"github.com/bluesky-social/indigo/atproto/identity"
	"github.com/bluesky-social/indigo/cmd/nexus/models"
	"github.com/bluesky-social/indigo/events"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

type Nexus struct {
	db     *gorm.DB
	logger *slog.Logger

	Dir       identity.Directory
	RelayHost string

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
	DBPath                     string
	RelayHost                  string
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
	db, err := gorm.Open(sqlite.Open(config.DBPath+"?_journal_mode=WAL&_busy_timeout=10000&_synchronous=NORMAL&_temp_store=MEMORY&_cache_size=8000&_wal_autocheckpoint=3000"), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Silent),
	})
	if err != nil {
		return nil, err
	}

	if err := db.AutoMigrate(&models.Repo{}, &models.RepoRecord{}, &models.OutboxBuffer{}, &models.ResyncBuffer{}, &models.FirehoseCursor{}, &models.ListReposCursor{}, &models.CollectionCursor{}); err != nil {
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

		Dir:       &cdir,
		RelayHost: config.RelayHost,

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
		RelayHost:         config.RelayHost,
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
		RelayHost:   config.RelayHost,
		Logger:      n.logger.With("component", "firehose"),
		Parallelism: firehoseParallelism,
		Callbacks:   rsc,
		GetCursor:   n.EventProcessor.ReadLastCursor,
	}

	n.Crawler = &Crawler{
		Logger:    n.logger.With("component", "crawler"),
		DB:        db,
		RelayHost: config.RelayHost,
	}

	if err := n.resetPartiallyResynced(); err != nil {
		return nil, err
	}

	return n, nil
}

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
