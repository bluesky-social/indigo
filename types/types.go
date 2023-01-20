package types

import (
	"time"

	"gorm.io/gorm"

	bsky "github.com/bluesky-social/indigo/api/bsky"
)

type FeedPost struct {
	gorm.Model
	Author      uint
	Rkey        string
	Cid         string
	UpCount     int64
	ReplyCount  int64
	RepostCount int64
	ReplyTo     uint
}

type RepostRecord struct {
	ID         uint `gorm:"primarykey"`
	CreatedAt  time.Time
	RecCreated string
	Post       uint
	Reposter   uint
	Author     uint
	RecCid     string
	Rkey       string
}

type ActorInfo struct {
	gorm.Model
	Uid         uint `gorm:"index"`
	Handle      string
	DisplayName string
	Did         string
	Following   int64
	Followers   int64
	Posts       int64
	DeclRefCid  string
	Type        string
	PDS         uint
}

func (ai *ActorInfo) ActorRef() *bsky.ActorRef_WithInfo {
	return &bsky.ActorRef_WithInfo{
		Did: ai.Did,
		Declaration: &bsky.SystemDeclRef{
			Cid:       ai.DeclRefCid,
			ActorType: ai.Type,
		},
		Handle:      ai.Handle,
		DisplayName: &ai.DisplayName,
	}
}

type VoteDir int

func (vd VoteDir) String() string {
	switch vd {
	case VoteDirUp:
		return "up"
	case VoteDirDown:
		return "down"
	default:
		return "<unknown>"
	}
}

const (
	VoteDirUp   = VoteDir(1)
	VoteDirDown = VoteDir(2)
)

type VoteRecord struct {
	gorm.Model
	Dir     VoteDir
	Voter   uint
	Post    uint
	Created string
	Rkey    string
	Cid     string
}

type FollowRecord struct {
	gorm.Model
	Follower uint
	Target   uint
	Rkey     string
	Cid      string
}
