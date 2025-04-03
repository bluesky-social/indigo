package models

import (
	"time"

	"gorm.io/gorm"
)

// TODO: revisit this
type DomainBan struct {
	gorm.Model
	Domain string `gorm:"unique"`
}

type HostStatus string

const (
	HostStatusActive = HostStatus("active")
	HostStatusIdle   = HostStatus("idle")
	HostStatusBanned = HostStatus("banned")
)

type Host struct {
	ID        uint64 `gorm:"column:id;primarykey"`
	CreatedAt time.Time
	UpdatedAt time.Time

	// hostname, without URL scheme. might include a port number if localhost, otherwise should not
	Hostname string `gorm:"column:hostname;uniqueIndex;not null"`

	// indicates ws:// not wss://
	NoSSL bool `gorm:"column:no_ssl;default:false"`

	// maximum number of active accounts
	AccountLimit int64 `gorm:"column:account_limit"`

	// TODO: ThrottleUntil time.Time

	// indicates this is a highly trusted PDS (different limits apply)
	Trusted bool `gorm:"column:trusted;default:false"`

	// enum of account status
	Status HostStatus `gorm:"column:status;default:active"`

	// negative number indicates no sequence recorded
	LastSeq      int64 `gorm:"column:last_seq"`
	AccountCount int64 `gorm:"column:account_count"`
}

func (Host) TableName() string {
	return "host"
}

type AccountStatus string

var (
	// AccountStatusActive is not in the spec but used internally
	AccountStatusActive = AccountStatus("active")

	AccountStatusDeactivated    = AccountStatus("deactivated")
	AccountStatusDeleted        = AccountStatus("deleted")
	AccountStatusDesynchronized = AccountStatus("desynchronized")
	AccountStatusSuspended      = AccountStatus("suspended")
	AccountStatusTakendown      = AccountStatus("takendown")
	AccountStatusThrottled      = AccountStatus("throttled")
	AccountStatusHostThrottled  = AccountStatus("host-throttled")
)

type Account struct {
	UID            uint64        `gorm:"column:uid;primarykey"`
	DID            string        `gorm:"column:did;uniqueIndex;not null"`
	HostID         uint64        `gorm:"column:host_id;not null"`
	Status         AccountStatus `gorm:"column:status;default:active"`
	UpstreamStatus AccountStatus `gorm:"column:upstream_status;default:active"`
	ThrottleUntil  time.Time     `gorm:"column:throttle_util"`
}

func (Account) TableName() string {
	return "account"
}

// This is a small extension table to `Account`, which holds fast-changing fields updated on every firehose event.
type AccountRepo struct {
	UID uint64 `gorm:"column:uid;primarykey"`
	Rev string `gorm:"column:rev;not null"`
	// The CID of the entire signed commit block. Sometimes called the "head"
	CommitCID string `gorm:"column:commit_cid;not null"`
	// The CID of the top of the repo MST, which is the 'data' field within the commit block. This becomes 'prevData'
	CommitData string `gorm:"column:commit_data;not null"`
}

func (AccountRepo) TableName() string {
	return "account_repo"
}
