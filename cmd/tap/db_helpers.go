package main

import (
	"github.com/bluesky-social/indigo/cmd/tap/models"
	"gorm.io/gorm"
)

func deleteRepo(tx *gorm.DB, did string) error {
	if err := tx.Delete(&models.RepoRecord{}, "did = ?", did).Error; err != nil {
		return err
	}
	if err := tx.Delete(&models.ResyncBuffer{}, "did = ?", did).Error; err != nil {
		return err
	}
	return tx.Delete(&models.Repo{}, "did = ?", did).Error
}
