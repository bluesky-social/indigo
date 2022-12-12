package schemagen

import (
	"encoding/json"

	comatprototypes "github.com/whyrusleeping/gosky/api/atproto"
)

// schema: app.bsky.graph.confirmation

// RECORDTYPE: GraphConfirmation
type GraphConfirmation struct {
	Originator *ActorRef                      `json:"originator" cborgen:"originator"`
	Assertion  *comatprototypes.RepoStrongRef `json:"assertion" cborgen:"assertion"`
	CreatedAt  string                         `json:"createdAt" cborgen:"createdAt"`
}

func (t *GraphConfirmation) MarshalJSON() ([]byte, error) {
	out := make(map[string]interface{})
	out["assertion"] = t.Assertion
	out["createdAt"] = t.CreatedAt
	out["originator"] = t.Originator
	return json.Marshal(out)
}
