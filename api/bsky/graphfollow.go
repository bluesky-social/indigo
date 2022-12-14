package schemagen

import (
	"github.com/whyrusleeping/gosky/lex/util"
)

// schema: app.bsky.graph.follow

func init() {
	util.RegisterType("app.bsky.graph.follow", GraphFollow{})
}

// RECORDTYPE: GraphFollow
type GraphFollow struct {
	LexiconTypeID string    `json:"$type" cborgen:"$type,const=app.bsky.graph.follow"`
	Subject       *ActorRef `json:"subject" cborgen:"subject"`
	CreatedAt     string    `json:"createdAt" cborgen:"createdAt"`
}
