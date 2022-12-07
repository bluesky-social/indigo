package schemagen

import (
	"encoding/json"

	comatprototypes "github.com/whyrusleeping/gosky/api/atproto"
)

// schema: app.bsky.feed.repost

type FeedRepost struct {
	Subject   *comatprototypes.RepoStrongRef `json:"subject"`
	CreatedAt string                         `json:"createdAt"`
}

func (t *FeedRepost) MarshalJSON() ([]byte, error) {
	out := make(map[string]interface{})
	out["createdAt"] = t.CreatedAt
	out["subject"] = t.Subject
	return json.Marshal(out)
}
