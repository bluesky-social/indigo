package schemagen

import (
	comatprototypes "github.com/bluesky-social/indigo/api/atproto"
	"github.com/bluesky-social/indigo/lex/util"
)

// schema: app.bsky.feed.trend

func init() {
	util.RegisterType("app.bsky.feed.trend", &FeedTrend{})
}

// RECORDTYPE: FeedTrend
type FeedTrend struct {
	LexiconTypeID string                         `json:"$type" cborgen:"$type,const=app.bsky.feed.trend"`
	CreatedAt     string                         `json:"createdAt" cborgen:"createdAt"`
	Subject       *comatprototypes.RepoStrongRef `json:"subject" cborgen:"subject"`
}
