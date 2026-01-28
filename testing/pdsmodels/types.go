package pdsmodels

// This file contains legacy PDS-related database model types. They are used in
// some event persister tests.

import (
	"time"

	"github.com/bluesky-social/indigo/models"

	"gorm.io/gorm"
)

type User struct {
	ID          models.Uid `gorm:"primarykey"`
	CreatedAt   time.Time
	UpdatedAt   time.Time
	DeletedAt   gorm.DeletedAt `gorm:"index"`
	Handle      string         `gorm:"uniqueIndex"`
	Password    string
	RecoveryKey string
	Email       string
	Did         string `gorm:"uniqueIndex"`
	PDS         uint
}

type Peering struct {
	gorm.Model
	Host     string
	Did      string
	Approved bool
}
