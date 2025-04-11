package models

import (
	"time"

	"gorm.io/gorm"
)

type DomainBan struct {
	gorm.Model
	Domain string `gorm:"unique"`
}

type HostStatus string

const (
	HostStatusActive    = HostStatus("active")
	HostStatusIdle      = HostStatus("idle")
	HostStatusOffline   = HostStatus("offline")
	HostStatusThrottled = HostStatus("throttled")
	HostStatusBanned    = HostStatus("banned")
)

type Host struct {
	ID uint64 `gorm:"column:id;primarykey"`

	// these fields are automatically managed by gorm (by convention)
	CreatedAt time.Time
	UpdatedAt time.Time

	// hostname, without URL scheme. if localhost, must include a port number; otherwise must not include port
	Hostname string `gorm:"column:hostname;uniqueIndex;not null"`

	// indicates ws:// not wss://
	NoSSL bool `gorm:"column:no_ssl;default:false"`

	// maximum number of active accounts
	AccountLimit int64 `gorm:"column:account_limit"`

	// TODO: ThrottleUntil time.Time

	// indicates this is a highly trusted host (PDS), and different rate limits apply
	Trusted bool `gorm:"column:trusted;default:false"`

	Status HostStatus `gorm:"column:status;default:active"`

	// the last sequence number persisted for this host. updated periodically, and at shutdown. negative number indicates no sequence recorded
	LastSeq int64 `gorm:"column:last_seq;default:-1"`

	// represents the number of accounts on the host, minus any in "deleted" state
	AccountCount int64 `gorm:"column:account_count;default:0"`
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
	AccountStatusHostThrottled  = AccountStatus("host-throttled") // TODO: not yet implemented

	// generic "not active, but not known" status
	AccountStatusInactive = AccountStatus("inactive")
)

type Account struct {
	UID uint64 `gorm:"column:uid;primarykey"`
	DID string `gorm:"column:did;uniqueIndex;not null"`

	// this is a reference to the ID field on Host; but it is not an explicit foreign key
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
	// references Account.UID, but not set up as a foreign key
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
