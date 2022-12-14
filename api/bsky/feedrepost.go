package schemagen

import (
	comatprototypes "github.com/whyrusleeping/gosky/api/atproto"
	"github.com/whyrusleeping/gosky/lex/util"
)

// schema: app.bsky.feed.repost

func init() {
	util.RegisterType("app.bsky.feed.repost", FeedRepost{})
}

// RECORDTYPE: FeedRepost
type FeedRepost struct {
	LexiconTypeID string                         `json:"$type" cborgen:"$type,const=app.bsky.feed.repost"`
	Subject       *comatprototypes.RepoStrongRef `json:"subject" cborgen:"subject"`
	CreatedAt     string                         `json:"createdAt" cborgen:"createdAt"`
}
