package models

import "time"

type RepoState string

const (
	RepoStatePending    RepoState = "pending"
	RepoStateBackfilling RepoState = "backfilling"
	RepoStateActive     RepoState = "active"
	RepoStateError      RepoState = "error"
)

type FilterDid struct {
	Did       string    `gorm:"primaryKey"`
	State     RepoState `gorm:"not null;default:'pending'"`
	Rev       string    `gorm:"type:text"` // Latest known revision
	ErrorMsg  string    `gorm:"type:text"` // Error message if state is error
	CreatedAt time.Time `gorm:"not null"`
	UpdatedAt time.Time `gorm:"not null"`
}

type BufferedEvt struct {
	ID         uint   `gorm:"primaryKey"`
	Did        string `gorm:"not null;index"`
	Collection string `gorm:"not null"`
	Rkey       string `gorm:"not null"`
	Action     string `gorm:"not null"`
	Cid        string `gorm:"type:text"`
	Record     string `gorm:"type:text"`
}
