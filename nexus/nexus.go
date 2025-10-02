package main

import (
	"context"
	"log/slog"

	"github.com/bluesky-social/indigo/nexus/models"
	"github.com/labstack/echo/v4"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

type Nexus struct {
	db     *gorm.DB
	echo   *echo.Echo
	logger *slog.Logger

	filterDids map[string]bool

	outbox *Outbox
}

type Op struct {
	DID        string `json:"did"`
	Collection string `json:"collection"`
	Rkey       string `json:"rkey"`
}

type NexusConfig struct {
	DBPath string
}

func NewNexus(config NexusConfig) (*Nexus, error) {
	// Open SQLite DB with GORM
	db, err := gorm.Open(sqlite.Open(config.DBPath), &gorm.Config{})
	if err != nil {
		return nil, err
	}

	// Auto-migrate the schema
	if err := db.AutoMigrate(&models.BufferedEvt{}, &models.FilterCollection{}, &models.FilterDid{}); err != nil {
		return nil, err
	}

	// Create Echo instance
	e := echo.New()
	e.HideBanner = true

	n := &Nexus{
		db:     db,
		echo:   e,
		logger: slog.Default().With("system", "nexus"),

		filterDids: make(map[string]bool),

		outbox: NewOutbox(db),
	}

	err = n.LoadFilters()
	if err != nil {
		return nil, err
	}

	// Register routes
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

	for _, f := range filterDids {
		n.filterDids[f.Did] = true
	}

	return nil
}
