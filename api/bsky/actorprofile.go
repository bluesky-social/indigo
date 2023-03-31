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
	LexiconTypeID string        `json:"$type" cborgen:"$type,const=app.bsky.actor.profile"`
	Avatar        *util.LexBlob `json:"avatar,omitempty" cborgen:"avatar,omitempty"`
	Banner        *util.LexBlob `json:"banner,omitempty" cborgen:"banner,omitempty"`
	Description   *string       `json:"description,omitempty" cborgen:"description,omitempty"`
	DisplayName   *string       `json:"displayName,omitempty" cborgen:"displayName,omitempty"`
}
