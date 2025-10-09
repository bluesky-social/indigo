package main

import (
	"context"
	"log/slog"
	"time"

	"github.com/bluesky-social/indigo/atproto/identity"
	"github.com/bluesky-social/indigo/nexus/models"
	"github.com/labstack/echo/v4"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

type Nexus struct {
	db     *gorm.DB
	echo   *echo.Echo
	logger *slog.Logger

	filter *StringSet
	Dir    identity.Directory

	outbox        *Outbox
	backfillQueue *BackfillQueue

	FirehoseConsumer *FirehoseConsumer
}

type NexusConfig struct {
	DBPath                     string
	RelayHost                  string
	FirehoseParallelism        int
	FirehosePersistCursorEvery int
}

func NewNexus(config NexusConfig) (*Nexus, error) {
	db, err := gorm.Open(sqlite.Open(config.DBPath), &gorm.Config{})
	if err != nil {
		return nil, err
	}

	if err := db.AutoMigrate(&models.BufferedEvt{}, &models.Did{}, &models.RepoRecord{}, &models.BackfillBuffer{}, &models.Cursor{}); err != nil {
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

		filter: NewStringSet(),
		Dir:    &cdir,

		outbox:        NewOutbox(db),
		backfillQueue: NewBackfillQueue(),
	}

	parallelism := config.FirehoseParallelism
	if parallelism == 0 {
		parallelism = 10
	}

	persistCursorEvery := config.FirehosePersistCursorEvery
	if persistCursorEvery == 0 {
		persistCursorEvery = 100
	}

	n.FirehoseConsumer = &FirehoseConsumer{
		RelayHost:          config.RelayHost,
		Filter:             n.filter,
		Logger:             n.logger.With("component", "firehose"),
		DB:                 db,
		Parallelism:        parallelism,
		PersistCursorEvery: persistCursorEvery,
		OnEvent:            n.handleEvent,
	}

	// run 50 backfill workers
	for i := 0; i < 50; i++ {
		go n.runBackfillWorker(context.Background(), i)
	}

	err = n.LoadFilters()
	if err != nil {
		return nil, err
	}

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

func (n *Nexus) LoadFilters() error {
	var dids []models.Did
	if err := n.db.Find(&dids).Error; err != nil {
		return err
	}

	didStrings := make([]string, 0, len(dids))
	for _, d := range dids {
		didStrings = append(didStrings, d.Did)

		if d.State == models.RepoStatePending || d.State == models.RepoStateBackfilling {
			n.queueBackfill(d.Did)
		}
	}

	n.filter.AddBatch(didStrings)
	return nil
}

func (n *Nexus) runBackfillWorker(ctx context.Context, workerID int) {
	logger := n.logger.With("worker", workerID)

	for {
		did := n.backfillQueue.Dequeue()
		logger.Info("processing backfill", "did", did)
		err := n.backfillDid(ctx, did)
		if err != nil {
			logger.Error("backfill failed", "did", did, "error", err)
		}
	}
}

func (n *Nexus) queueBackfill(did string) {
	depth := n.backfillQueue.Enqueue(did)
	n.logger.Info("queued backfill", "did", did, "queue_depth", depth)
}

func (n *Nexus) GetRepoState(did string) (models.RepoState, error) {
	var d models.Did
	if err := n.db.First(&d, "did = ?", did).Error; err != nil {
		return "", err
	}
	return d.State, nil
}

func (n *Nexus) UpdateRepoState(did string, state models.RepoState, rev string, errorMsg string) error {
	return n.db.Model(&models.Did{}).
		Where("did = ?", did).
		Updates(map[string]interface{}{
			"state":     state,
			"rev":       rev,
			"error_msg": errorMsg,
		}).Error
}

func (n *Nexus) handleEvent(ctx context.Context, op *Op) error {
	return n.outbox.Send(op)
}
