package schemagen

import (
	"github.com/whyrusleeping/gosky/lex/util"
)

// schema: app.bsky.actor.profile

func init() {
	util.RegisterType("app.bsky.actor.profile", ActorProfile{})
}

// RECORDTYPE: ActorProfile
type ActorProfile struct {
	LexiconTypeID string  `json:"$type" cborgen:"$type,const=app.bsky.actor.profile"`
	DisplayName   string  `json:"displayName" cborgen:"displayName"`
	Description   *string `json:"description" cborgen:"description"`
}
