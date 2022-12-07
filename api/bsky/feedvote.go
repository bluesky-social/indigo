package schemagen

import (
	"encoding/json"

	comatprototypes "github.com/whyrusleeping/gosky/api/atproto"
)

// schema: app.bsky.feed.vote

type FeedVote struct {
	CreatedAt string                         `json:"createdAt"`
	Subject   *comatprototypes.RepoStrongRef `json:"subject"`
	Direction string                         `json:"direction"`
}

func (t *FeedVote) MarshalJSON() ([]byte, error) {
	out := make(map[string]interface{})
	out["createdAt"] = t.CreatedAt
	out["direction"] = t.Direction
	out["subject"] = t.Subject
	return json.Marshal(out)
}
