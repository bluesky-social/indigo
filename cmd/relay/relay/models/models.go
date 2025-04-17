package models

import (
	"time"
)

type DomainBan struct {
	ID uint64 `gorm:"column:id;primarykey" json:"id"`
	// CreatedAt is automatically managed by gorm (by convention)
	CreatedAt time.Time `json:"createdAt"`

	Domain string `gorm:"unique" json:"domain"`
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
	ID uint64 `gorm:"column:id;primarykey" json:"id"`

	// these fields are automatically managed by gorm (by convention)
	CreatedAt time.Time `json:"createdAt"`
	UpdatedAt time.Time `json:"updatedAt"`

	// hostname, without URL scheme. if localhost, must include a port number; otherwise must not include port
	Hostname string `gorm:"column:hostname;uniqueIndex;not null" json:"hostname"`

	// indicates ws:// not wss://
	NoSSL bool `gorm:"column:no_ssl;default:false" json:"noSSL"`

	// maximum number of active accounts
	AccountLimit int64 `gorm:"column:account_limit" json:"accountLimit"`

	// indicates this is a highly trusted host (PDS), and different rate limits apply
	Trusted bool `gorm:"column:trusted;default:false" json:"trusted"`

	Status HostStatus `gorm:"column:status;default:active" json:"status"`

	// the last sequence number persisted for this host. updated periodically, and at shutdown. negative number indicates no sequence recorded
	LastSeq int64 `gorm:"column:last_seq;default:-1" json:"lastSeq"`

	// represents the number of accounts on the host, minus any in "deleted" state
	AccountCount int64 `gorm:"column:account_count;default:0" json:"accountCount"`
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
	UID uint64 `gorm:"column:uid;primarykey" json:"uid"`
	DID string `gorm:"column:did;uniqueIndex;not null" json:"did"`

	// this is a reference to the ID field on Host; but it is not an explicit foreign key
	HostID         uint64        `gorm:"column:host_id;not null" json:"hostID"`
	Status         AccountStatus `gorm:"column:status;not null;default:active" json:"status"`
	UpstreamStatus AccountStatus `gorm:"column:upstream_status;not null;default:active" json:"upstreamStatus"`
}

func (Account) TableName() string {
	return "account"
}

// This is a small extension table to `Account`, which holds fast-changing fields updated on every firehose event.
type AccountRepo struct {
	// references Account.UID, but not set up as a foreign key
	UID uint64 `gorm:"column:uid;primarykey" json:"uid"`
	Rev string `gorm:"column:rev;not null" json:"rev"`

	// The CID of the entire signed commit block. Sometimes called the "head"
	CommitCID string `gorm:"column:commit_cid;not null" json:"commitCID"`

	// The CID of the top of the repo MST, which is the 'data' field within the commit block. This becomes 'prevData'
	CommitDataCID string `gorm:"column:commit_data_cid;not null" json:"commitDataCID"`
}

func (AccountRepo) TableName() string {
	return "account_repo"
}
