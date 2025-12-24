package main

import (
	"context"
	"log/slog"
	"time"

	"github.com/bluesky-social/indigo/atproto/identity"
	"github.com/bluesky-social/indigo/atproto/syntax"
	"github.com/bluesky-social/indigo/cmd/tap/models"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type LabelerManager struct {
	logger *slog.Logger
	db     *gorm.DB
	idDir  identity.Directory
	events *EventManager

	parallelism        int
	cursorSaveInterval time.Duration
	activeSubscribers  map[string]context.CancelFunc
}

func (lm *LabelerManager) EnsureLabeler(ctx context.Context, did string) error {
	labeler := models.Labeler{
		DID:        did,
		ServiceURL: nil,
		State:      "pending",
	}
	if err := lm.db.WithContext(ctx).Clauses(clause.OnConflict{DoNothing: true}).Create(&labeler).Error; err != nil {
		lm.logger.Error("failed to track labeler", "did", did, "error", err)
		return err
	}
	return nil
}

func (lm *LabelerManager) Run(ctx context.Context) {
	lm.logger.Info("starting labeler manager")

	now := time.Now().Unix()
	var labelers []models.Labeler
	if err := lm.db.Where("state IN (?, ?) AND (retry_after = 0 OR retry_after < ?)", "active", "error", now).Limit(lm.parallelism).Find(&labelers).Error; err != nil {
		lm.logger.Error("failed to load existing labelers", "error", err)
	} else {
		for _, labeler := range labelers {
			subCtx, cancel := context.WithCancel(ctx)
			lm.activeSubscribers[labeler.DID] = cancel

			lp := &LabelProcessor{
				logger:             lm.logger.With("labeler", labeler.DID),
				db:                 lm.db,
				events:             lm.events,
				idDir:              lm.idDir,
				cursorSaveInterval: lm.cursorSaveInterval,
				labelerDID:         labeler.DID,
				serviceURL:         labeler.ServiceURL,
			}
			go lp.Run(subCtx)
			lm.logger.Info("starting label processor", "did", labeler.DID)
		}
	}

	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			for did, cancel := range lm.activeSubscribers {
				lm.logger.Info("stopping labeler subscriber", "did", did)
				cancel()
			}
			return
		case <-ticker.C:
			lm.processPendingLabelers(ctx)
			lm.reconcileActiveLabelers(ctx)
		}
	}
}

func (lm *LabelerManager) processPendingLabelers(ctx context.Context) {
	var pendingLabelers []models.Labeler
	if err := lm.db.Where("state = ?", "pending").Find(&pendingLabelers).Error; err != nil {
		lm.logger.Error("failed to query pending labelers", "error", err)
		return
	}

	for _, labeler := range pendingLabelers {
		did, err := syntax.ParseDID(labeler.DID)
		if err != nil {
			lm.logger.Error("invalid labeler DID", "did", labeler.DID, "error", err)
			continue
		}

		ident, err := lm.idDir.LookupDID(ctx, did)
		if err != nil {
			lm.logger.Error("failed to resolve labeler DID", "did", labeler.DID, "error", err)
			continue
		}

		serviceURL := ident.GetServiceEndpoint("atproto_labeler")
		if serviceURL == "" {
			continue
		}

		if err := lm.db.Model(&labeler).Updates(map[string]any{
			"service_url": &serviceURL,
			"state":       "active",
		}).Error; err != nil {
			lm.logger.Error("failed to activate labeler", "did", labeler.DID, "error", err)
		}
	}
}

func (lm *LabelerManager) reconcileActiveLabelers(ctx context.Context) {
	now := time.Now().Unix()
	var activeLabelers []models.Labeler
	if err := lm.db.Where("state IN (?, ?) AND (retry_after = 0 OR retry_after < ?)", "active", "error", now).Find(&activeLabelers).Error; err != nil {
		lm.logger.Error("failed to query active labelers", "error", err)
		return
	}

	activeDIDs := make(map[string]bool)
	for _, labeler := range activeLabelers {
		activeDIDs[labeler.DID] = true
	}

	for did, cancel := range lm.activeSubscribers {
		if !activeDIDs[did] {
			lm.logger.Info("stopping subscriber for removed labeler", "did", did)
			cancel()
			delete(lm.activeSubscribers, did)
		}
	}

	skipped := 0
	for _, labeler := range activeLabelers {
		if _, exists := lm.activeSubscribers[labeler.DID]; !exists {
			if len(lm.activeSubscribers) >= lm.parallelism {
				skipped++
				continue
			}

			subCtx, cancel := context.WithCancel(ctx)
			lm.activeSubscribers[labeler.DID] = cancel

			lp := &LabelProcessor{
				logger:             lm.logger.With("labeler", labeler.DID),
				db:                 lm.db,
				events:             lm.events,
				idDir:              lm.idDir,
				cursorSaveInterval: lm.cursorSaveInterval,
				labelerDID:         labeler.DID,
				serviceURL:         labeler.ServiceURL,
			}
			go lp.Run(subCtx)
			lm.logger.Info("starting label processor", "did", labeler.DID)
		}
	}

	if skipped > 0 {
		lm.logger.Warn("skipped labelers due to parallelism limit", "skipped", skipped, "limit", lm.parallelism, "active", len(lm.activeSubscribers))
	}
}
