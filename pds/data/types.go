package data

import (
	"github.com/bluesky-social/indigo/models"
	"gorm.io/gorm"
	"time"
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
