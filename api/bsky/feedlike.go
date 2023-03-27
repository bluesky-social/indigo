package bsky

import (
	comatprototypes "github.com/bluesky-social/indigo/api/atproto"
)

// schema: app.bsky.feed.like

func init() {
}

// RECORDTYPE: FeedLike
type FeedLike struct {
	LexiconTypeID string                         `json:"$type" cborgen:"$type,const=app.bsky.feed.like"`
	CreatedAt     string                         `json:"createdAt" cborgen:"createdAt"`
	Subject       *comatprototypes.RepoStrongRef `json:"subject" cborgen:"subject"`
}
