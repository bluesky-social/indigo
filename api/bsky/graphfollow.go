package schemagen

import (
	"encoding/json"
)

// schema: app.bsky.graph.follow

// RECORDTYPE: GraphFollow
type GraphFollow struct {
	Subject   *ActorRef `json:"subject" cborgen:"subject"`
	CreatedAt string    `json:"createdAt" cborgen:"createdAt"`
}

func (t *GraphFollow) MarshalJSON() ([]byte, error) {
	out := make(map[string]interface{})
	out["createdAt"] = t.CreatedAt
	out["subject"] = t.Subject
	return json.Marshal(out)
}
