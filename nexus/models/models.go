package models

type RepoState string

const (
	RepoStatePending   RepoState = "pending"
	RepoStateResyncing RepoState = "resyncing"
	RepoStateActive    RepoState = "active"
	RepoStateDesynced  RepoState = "desynced"
	RepoStateError     RepoState = "error"
)

type Repo struct {
	Did      string    `gorm:"primaryKey"`
	State    RepoState `gorm:"not null;default:'pending';index"`
	Rev      string    `gorm:"type:text"`
	PrevData string    `gorm:"type:text"`
	ErrorMsg string    `gorm:"type:text"`
}

type OutboxBuffer struct {
	ID   uint   `gorm:"primaryKey"`
	Data string `gorm:"type:text;not null"` // JSON-encoded operations
}

type ResyncBuffer struct {
	ID   uint   `gorm:"primaryKey"`
	Did  string `gorm:"not null;index"`
	Data string `gorm:"type:text;not null"` // JSON-encoded Commit
}

type RepoRecord struct {
	Did        string `gorm:"primaryKey"`
	Collection string `gorm:"primaryKey"`
	Rkey       string `gorm:"primaryKey"`
	Cid        string `gorm:"not null"`
}

type Cursor struct {
	Host   string `gorm:"primaryKey"`
	Cursor int64  `gorm:"not null"`
}
