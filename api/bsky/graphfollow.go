package bsky

import (
	"github.com/bluesky-social/indigo/lex/util"
)

// schema: app.bsky.graph.follow

func init() {
	util.RegisterType("app.bsky.graph.follow", &GraphFollow{})
}

// RECORDTYPE: GraphFollow
type GraphFollow struct {
	LexiconTypeID string `json:"$type,const=app.bsky.graph.follow" cborgen:"$type,const=app.bsky.graph.follow"`
	CreatedAt     string `json:"createdAt" cborgen:"createdAt"`
	Subject       string `json:"subject" cborgen:"subject"`
}
