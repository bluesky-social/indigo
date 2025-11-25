package main

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/bluesky-social/indigo/cmd/nexus/models"
	"gorm.io/gorm"
)

type ResyncBuffer struct {
	db     *gorm.DB
	events *EventManager
}

func (rb *ResyncBuffer) add(commit *Commit) error {
	jsonData, err := json.Marshal(commit)
	if err != nil {
		return err
	}
	return rb.db.Create(&models.ResyncBuffer{
		Did:  commit.Did,
		Data: string(jsonData),
	}).Error
}

func (rb *ResyncBuffer) drain(ctx context.Context, did string) error {
	var bufferedEvts []models.ResyncBuffer
	if err := rb.db.Where("did = ?", did).Order("id ASC").Find(&bufferedEvts).Error; err != nil {
		return fmt.Errorf("failed to load buffered events: %w", err)
	}

	if len(bufferedEvts) == 0 {
		return nil
	}

	for _, evt := range bufferedEvts {
		var commit Commit
		if err := json.Unmarshal([]byte(evt.Data), &commit); err != nil {
			return fmt.Errorf("failed to unmarshal buffered event: %w", err)
		}

		if err := rb.events.AddCommit(&commit, func(tx *gorm.DB) error {
			return tx.Delete(&models.ResyncBuffer{}, "id = ?", evt.ID).Error
		}); err != nil {
			return err
		}
	}

	return nil
}
