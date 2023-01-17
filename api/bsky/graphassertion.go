package schemagen

import (
	"github.com/whyrusleeping/gosky/lex/util"
)

// schema: app.bsky.graph.assertion

func init() {
	util.RegisterType("app.bsky.graph.assertion", &GraphAssertion{})
}

// RECORDTYPE: GraphAssertion
type GraphAssertion struct {
	LexiconTypeID string    `json:"$type" cborgen:"$type,const=app.bsky.graph.assertion"`
	Assertion     string    `json:"assertion" cborgen:"assertion"`
	CreatedAt     string    `json:"createdAt" cborgen:"createdAt"`
	Subject       *ActorRef `json:"subject" cborgen:"subject"`
}
