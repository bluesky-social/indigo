package schemagen

import (
	"encoding/json"
)

// schema: app.bsky.graph.assertion

type GraphAssertion struct {
	Subject   *ActorRef `json:"subject"`
	CreatedAt string    `json:"createdAt"`
	Assertion string    `json:"assertion"`
}

func (t *GraphAssertion) MarshalJSON() ([]byte, error) {
	out := make(map[string]interface{})
	out["assertion"] = t.Assertion
	out["createdAt"] = t.CreatedAt
	out["subject"] = t.Subject
	return json.Marshal(out)
}
