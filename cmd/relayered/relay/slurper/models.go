package slurper

import (
	"sync"
	"time"

	"github.com/bluesky-social/indigo/atproto/syntax"

	"github.com/ipfs/go-cid"
	"gorm.io/gorm"
)

type DomainBan struct {
	gorm.Model
	Domain string `gorm:"unique"`
}

type AccountPreviousState struct {
	Uid uint64 `gorm:"column:uid;primaryKey"`
	Cid DbCID  `gorm:"column:cid"`
	Rev string `gorm:"column:rev"`
	Seq int64  `gorm:"column:seq"`
}

func (ups *AccountPreviousState) GetCid() cid.Cid {
	return ups.Cid.CID
}
func (ups *AccountPreviousState) GetRev() syntax.TID {
	xt, _ := syntax.ParseTID(ups.Rev)
	return xt
}

type PDS struct {
	gorm.Model

	Host       string `gorm:"unique"`
	SSL        bool
	Cursor     int64
	Registered bool
	Blocked    bool

	RateLimit float64

	RepoCount int64
	RepoLimit int64

	HourlyEventLimit int64
	DailyEventLimit  int64
}

type Account struct {
	ID        uint64 `gorm:"primarykey"`
	CreatedAt time.Time
	UpdatedAt time.Time
	DeletedAt gorm.DeletedAt `gorm:"index"`
	Did       string         `gorm:"uniqueIndex"`
	PDS       uint           // foreign key on models.PDS.ID

	// TakenDown is set to true if the user in question has been taken down by an admin action at this relay.
	// A user in this state will have all future events related to it dropped
	// and no data about this user will be served.
	TakenDown bool

	// UpstreamStatus is the state of the user as reported by the upstream PDS through #account messages.
	// Additionally, the non-standard string "active" is set to represent an upstream #account message with the active bool true.
	UpstreamStatus string `gorm:"index"`

	lk sync.Mutex
}

func (account *Account) GetDid() string {
	return account.Did
}

func (account *Account) GetUid() uint64 {
	return account.ID
}

func (account *Account) SetTakenDown(v bool) {
	account.lk.Lock()
	defer account.lk.Unlock()
	account.TakenDown = v
}

func (account *Account) GetTakenDown() bool {
	account.lk.Lock()
	defer account.lk.Unlock()
	return account.TakenDown
}

func (account *Account) SetPDS(pdsId uint) {
	account.lk.Lock()
	defer account.lk.Unlock()
	account.PDS = pdsId
}

func (account *Account) GetPDS() uint {
	account.lk.Lock()
	defer account.lk.Unlock()
	return account.PDS
}

func (account *Account) SetUpstreamStatus(v string) {
	account.lk.Lock()
	defer account.lk.Unlock()
	account.UpstreamStatus = v
}

func (account *Account) GetUpstreamStatus() string {
	account.lk.Lock()
	defer account.lk.Unlock()
	return account.UpstreamStatus
}
