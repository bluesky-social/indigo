package schemagen

import (
	comatprototypes "github.com/whyrusleeping/gosky/api/atproto"
	"github.com/whyrusleeping/gosky/lex/util"
)

// schema: app.bsky.feed.trend

func init() {
	util.RegisterType("app.bsky.feed.trend", FeedTrend{})
}

// RECORDTYPE: FeedTrend
type FeedTrend struct {
	LexiconTypeID string                         `json:"$type" cborgen:"$type,const=app.bsky.feed.trend"`
	Subject       *comatprototypes.RepoStrongRef `json:"subject" cborgen:"subject"`
	CreatedAt     string                         `json:"createdAt" cborgen:"createdAt"`
}
