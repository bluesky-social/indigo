package schemagen

import (
	"encoding/json"
)

// schema: app.bsky.graph.assertion

// RECORDTYPE: GraphAssertion
type GraphAssertion struct {
	Assertion string    `json:"assertion" cborgen:"assertion"`
	Subject   *ActorRef `json:"subject" cborgen:"subject"`
	CreatedAt string    `json:"createdAt" cborgen:"createdAt"`
}

func (t *GraphAssertion) MarshalJSON() ([]byte, error) {
	out := make(map[string]interface{})
	out["assertion"] = t.Assertion
	out["createdAt"] = t.CreatedAt
	out["subject"] = t.Subject
	return json.Marshal(out)
}
