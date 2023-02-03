package bsky

import (
	"github.com/bluesky-social/indigo/lex/util"
)

// schema: app.bsky.system.declaration

func init() {
	util.RegisterType("app.bsky.system.declaration", &SystemDeclaration{})
}

// RECORDTYPE: SystemDeclaration
type SystemDeclaration struct {
	LexiconTypeID string `json:"$type" cborgen:"$type,const=app.bsky.system.declaration"`
	ActorType     string `json:"actorType" cborgen:"actorType"`
}
