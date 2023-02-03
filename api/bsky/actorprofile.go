package bsky

import (
	"github.com/bluesky-social/indigo/lex/util"
)

// schema: app.bsky.actor.profile

func init() {
	util.RegisterType("app.bsky.actor.profile", &ActorProfile{})
}

// RECORDTYPE: ActorProfile
type ActorProfile struct {
	LexiconTypeID string     `json:"$type" cborgen:"$type,const=app.bsky.actor.profile"`
	Avatar        *util.Blob `json:"avatar,omitempty" cborgen:"avatar"`
	Banner        *util.Blob `json:"banner,omitempty" cborgen:"banner"`
	Description   *string    `json:"description,omitempty" cborgen:"description"`
	DisplayName   string     `json:"displayName" cborgen:"displayName"`
}
