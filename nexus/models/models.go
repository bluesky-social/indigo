package models

type RepoState string

const (
	RepoStatePending     RepoState = "pending"
	RepoStateDesynced    RepoState = "desynced"
	RepoStateResyncing   RepoState = "resyncing"
	RepoStateActive      RepoState = "active"
	RepoStateTakendown   RepoState = "takendown"
	RepoStateSuspended   RepoState = "suspended"
	RepoStateDeactivated RepoState = "deactivated"
	RepoStateError       RepoState = "error"
)

type AccountStatus string

const (
	AccountStatusActive      AccountStatus = "active"
	AccountStatusTakendown   AccountStatus = "takendown"
	AccountStatusSuspended   AccountStatus = "suspended"
	AccountStatusDeactivated AccountStatus = "deactivated"
	AccountStatusDeleted     AccountStatus = "deleted"
)

type Repo struct {
	Did        string        `gorm:"primaryKey"`
	State      RepoState     `gorm:"not null;default:'pending';index"`
	Status     AccountStatus `gorm:"not null;default:'active'"`
	Handle     string        `gorm:"type:text"`
	Rev        string        `gorm:"type:text"`
	PrevData   string        `gorm:"type:text"`
	ErrorMsg   string        `gorm:"type:text"`
	RetryCount int           `gorm:"not null;default:0"`
	RetryAfter int64         `gorm:"not null;default:0"` // Unix timestamp
}

type OutboxBuffer struct {
	ID   uint   `gorm:"primaryKey"`
	Live bool   `gorm:"not null"`
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

type FirehoseCursor struct {
	Host   string `gorm:"primaryKey"`
	Cursor int64  `gorm:"not null"`
}

type ListReposCursor struct {
	Host   string `gorm:"primaryKey"`
	Cursor string `gorm:"not null"`
}
