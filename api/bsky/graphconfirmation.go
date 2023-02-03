package bsky

import (
	comatprototypes "github.com/bluesky-social/indigo/api/atproto"
	"github.com/bluesky-social/indigo/lex/util"
)

// schema: app.bsky.graph.confirmation

func init() {
	util.RegisterType("app.bsky.graph.confirmation", &GraphConfirmation{})
}

// RECORDTYPE: GraphConfirmation
type GraphConfirmation struct {
	LexiconTypeID string                         `json:"$type" cborgen:"$type,const=app.bsky.graph.confirmation"`
	Assertion     *comatprototypes.RepoStrongRef `json:"assertion" cborgen:"assertion"`
	CreatedAt     string                         `json:"createdAt" cborgen:"createdAt"`
	Originator    *ActorRef                      `json:"originator" cborgen:"originator"`
}
