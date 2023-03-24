package models

import (
	"time"
)

type ModerationAction struct {
	ID             uint64 `gorm:"primaryKey"`
	Action         string `gorm:"not null"`
	SubjectType    string `gorm:"not null"`
	SubjectDid     string `gorm:"not null"`
	SubjectUri     *string
	SubjectCid     *string
	Reason         string    `gorm:"not null"`
	CreatedAt      time.Time `gorm:"not null"`
	CreatedByDid   string    `gorm:"not null"`
	ReversedAt     *time.Time
	ReversedByDid  *string
	ReversedReason *string
}

type ModerationReport struct {
	ID            uint64 `gorm:"primaryKey"`
	SubjectType   string `gorm:"not null"`
	SubjectDid    string `gorm:"not null"`
	SubjectUri    *string
	SubjectCid    *string
	ReasonType    string `gorm:"not null"`
	Reason        *string
	ReportedByDid string    `gorm:"not null"`
	CreatedAt     time.Time `gorm:"not null"`
}

type ModerationReportResolution struct {
	ReportId     uint64    `gorm:"primaryKey"`
	ActionId     uint64    `gorm:"primaryKey;index:"`
	CreatedAt    time.Time `gorm:"not null"`
	CreatedByDid string    `gorm:"not null"`
}
