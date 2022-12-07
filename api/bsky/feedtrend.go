package schemagen

import (
	"encoding/json"

	comatprototypes "github.com/whyrusleeping/gosky/api/atproto"
)

// schema: app.bsky.feed.trend

type FeedTrend struct {
	Subject   *comatprototypes.RepoStrongRef `json:"subject"`
	CreatedAt string                         `json:"createdAt"`
}

func (t *FeedTrend) MarshalJSON() ([]byte, error) {
	out := make(map[string]interface{})
	out["createdAt"] = t.CreatedAt
	out["subject"] = t.Subject
	return json.Marshal(out)
}
