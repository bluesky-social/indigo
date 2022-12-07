package schemagen

import (
	"encoding/json"

	comatprototypes "github.com/whyrusleeping/gosky/api/atproto"
)

// schema: app.bsky.graph.confirmation

type GraphConfirmation struct {
	Assertion  *comatprototypes.RepoStrongRef `json:"assertion"`
	CreatedAt  string                         `json:"createdAt"`
	Originator *ActorRef                      `json:"originator"`
}

func (t *GraphConfirmation) MarshalJSON() ([]byte, error) {
	out := make(map[string]interface{})
	out["assertion"] = t.Assertion
	out["createdAt"] = t.CreatedAt
	out["originator"] = t.Originator
	return json.Marshal(out)
}
