package bsky

import (
	comatprototypes "github.com/bluesky-social/indigo/api/atproto"
	"github.com/bluesky-social/indigo/lex/util"
)

// schema: app.bsky.feed.repost

func init() {
	util.RegisterType("app.bsky.feed.repost", &FeedRepost{})
}

// RECORDTYPE: FeedRepost
type FeedRepost struct {
	LexiconTypeID string                         `json:"$type" cborgen:"$type,const=app.bsky.feed.repost"`
	CreatedAt     string                         `json:"createdAt" cborgen:"createdAt"`
	Subject       *comatprototypes.RepoStrongRef `json:"subject" cborgen:"subject"`
}
