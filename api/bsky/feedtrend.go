package schemagen

import (
	"encoding/json"

	comatprototypes "github.com/whyrusleeping/gosky/api/atproto"
)

// schema: app.bsky.feed.trend

// RECORDTYPE: FeedTrend
type FeedTrend struct {
	Subject   *comatprototypes.RepoStrongRef `json:"subject" cborgen:"subject"`
	CreatedAt string                         `json:"createdAt" cborgen:"createdAt"`
}

func (t *FeedTrend) MarshalJSON() ([]byte, error) {
	out := make(map[string]interface{})
	out["createdAt"] = t.CreatedAt
	out["subject"] = t.Subject
	return json.Marshal(out)
}
