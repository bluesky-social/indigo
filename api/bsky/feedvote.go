package schemagen

import (
	"encoding/json"

	comatprototypes "github.com/whyrusleeping/gosky/api/atproto"
)

// schema: app.bsky.feed.vote

// RECORDTYPE: FeedVote
type FeedVote struct {
	Subject   *comatprototypes.RepoStrongRef `json:"subject" cborgen:"subject"`
	Direction string                         `json:"direction" cborgen:"direction"`
	CreatedAt string                         `json:"createdAt" cborgen:"createdAt"`
}

func (t *FeedVote) MarshalJSON() ([]byte, error) {
	out := make(map[string]interface{})
	out["createdAt"] = t.CreatedAt
	out["direction"] = t.Direction
	out["subject"] = t.Subject
	return json.Marshal(out)
}
