package models

import (
	"database/sql"

	"gorm.io/gorm"

	bsky "github.com/bluesky-social/indigo/api/bsky"
)

type ActorInfo struct {
	gorm.Model
	Uid         Uid            `gorm:"uniqueindex"`
	Handle      sql.NullString `gorm:"index"`
	DisplayName string
	Did         string `gorm:"uniqueindex"`
	Following   int64
	Followers   int64
	Posts       int64
	Type        string
	PDS         uint
	ValidHandle bool `gorm:"default:true"`
}

func (ai *ActorInfo) ActorRef() *bsky.ActorDefs_ProfileViewBasic {
	return &bsky.ActorDefs_ProfileViewBasic{
		Did:         ai.Did,
		Handle:      ai.Handle.String,
		DisplayName: &ai.DisplayName,
	}
}

// TODO: this is just s stub; needs to populate more info
func (ai *ActorInfo) ActorView() *bsky.ActorDefs_ProfileView {
	return &bsky.ActorDefs_ProfileView{
		Did:         ai.Did,
		Handle:      ai.Handle.String,
		DisplayName: &ai.DisplayName,
	}
}
