package schemagen

import (
	comatprototypes "github.com/whyrusleeping/gosky/api/atproto"
	"github.com/whyrusleeping/gosky/lex/util"
)

// schema: app.bsky.feed.vote

func init() {
	util.RegisterType("app.bsky.feed.vote", FeedVote{})
}

// RECORDTYPE: FeedVote
type FeedVote struct {
	LexiconTypeID string                         `json:"$type" cborgen:"$type,const=app.bsky.feed.vote"`
	Subject       *comatprototypes.RepoStrongRef `json:"subject" cborgen:"subject"`
	Direction     string                         `json:"direction" cborgen:"direction"`
	CreatedAt     string                         `json:"createdAt" cborgen:"createdAt"`
}
