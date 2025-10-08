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

	RelayHost string
}

type Op struct {
	Did        string                 `json:"did"`
	Collection string                 `json:"collection"`
	Rkey       string                 `json:"rkey"`
	Action     string                 `json:"action"`
	Record     map[string]interface{} `json:"record,omitempty"`
	Cid        string                 `json:"cid,omitempty"`
}

type NexusConfig struct {
	DBPath    string
	RelayHost string
}

func NewNexus(config NexusConfig) (*Nexus, error) {
	db, err := gorm.Open(sqlite.Open(config.DBPath), &gorm.Config{})
	if err != nil {
		return nil, err
	}

	if err := db.AutoMigrate(&models.BufferedEvt{}, &models.FilterDid{}, &models.RepoRecord{}, &models.BackfillBuffer{}, &models.Cursor{}); err != nil {
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

		RelayHost: config.RelayHost,
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
	var filterDids []models.FilterDid
	if err := n.db.Find(&filterDids).Error; err != nil {
		return err
	}

	dids := make([]string, 0, len(filterDids))
	for _, f := range filterDids {
		dids = append(dids, f.Did)

		if f.State == models.RepoStatePending || f.State == models.RepoStateBackfilling {
			n.queueBackfill(f.Did)
		}
	}

	n.filter.AddBatch(dids)
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
	var filterDid models.FilterDid
	if err := n.db.First(&filterDid, "did = ?", did).Error; err != nil {
		return "", err
	}
	return filterDid.State, nil
}

func (n *Nexus) UpdateRepoState(did string, state models.RepoState, rev string, errorMsg string) error {
	return n.db.Model(&models.FilterDid{}).
		Where("did = ?", did).
		Updates(map[string]interface{}{
			"state":     state,
			"rev":       rev,
			"error_msg": errorMsg,
		}).Error
}

func (n *Nexus) ReadLastCursor(ctx context.Context) (int64, error) {
	var cursor models.Cursor
	if err := n.db.Where("host = ?", n.RelayHost).First(&cursor).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			n.logger.Info("no pre-existing cursor in database", "relayHost", n.RelayHost)
			return 0, nil
		}
		return 0, err
	}
	return cursor.Cursor, nil
}

func (n *Nexus) PersistCursor(ctx context.Context, seq int64) error {
	if seq <= 0 {
		return nil
	}

	cursor := models.Cursor{
		Host:   n.RelayHost,
		Cursor: seq,
	}

	return n.db.Save(&cursor).Error
}
