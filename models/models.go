package models

import (
	"time"

	"gorm.io/gorm"

	bsky "github.com/bluesky-social/indigo/api/bsky"
	"github.com/bluesky-social/indigo/util"
	"github.com/bluesky-social/indigo/xrpc"
)

type FeedPost struct {
	gorm.Model
	Author      util.Uid `gorm:"index:idx_feedpost_rkey,unique"`
	Rkey        string   `gorm:"index:idx_feedpost_rkey,unique"`
	Cid         string
	UpCount     int64
	ReplyCount  int64
	RepostCount int64
	ReplyTo     uint
	Missing     bool
	Deleted     bool
}

type RepostRecord struct {
	ID         uint `gorm:"primarykey"`
	CreatedAt  time.Time
	RecCreated string
	Post       uint
	Reposter   util.Uid
	Author     util.Uid
	RecCid     string
	Rkey       string
}

type ActorInfo struct {
	gorm.Model
	Uid         util.Uid `gorm:"uniqueindex"`
	Handle      string
	DisplayName string
	Did         string `gorm:"uniqueindex"`
	Following   int64
	Followers   int64
	Posts       int64
	Type        string
	PDS         uint
}

func (ai *ActorInfo) ActorRef() *bsky.ActorDefs_ProfileViewBasic {
	return &bsky.ActorDefs_ProfileViewBasic{
		Did:         ai.Did,
		Handle:      ai.Handle,
		DisplayName: &ai.DisplayName,
	}
}

// TODO: this is just s stub; needs to populate more info
func (ai *ActorInfo) ActorView() *bsky.ActorDefs_ProfileView {
	return &bsky.ActorDefs_ProfileView{
		Did:         ai.Did,
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
	Voter   util.Uid
	Post    uint
	Created string
	Rkey    string
	Cid     string
}

type FollowRecord struct {
	gorm.Model
	Follower util.Uid
	Target   util.Uid
	Rkey     string
	Cid      string
}

type PDS struct {
	gorm.Model

	Host       string
	Did        string
	SSL        bool
	Cursor     int64
	Registered bool
}

func ClientForPds(pds *PDS) *xrpc.Client {
	if pds.SSL {
		return &xrpc.Client{
			Host: "https://" + pds.Host,
		}
	}

	return &xrpc.Client{
		Host: "http://" + pds.Host,
	}
}
