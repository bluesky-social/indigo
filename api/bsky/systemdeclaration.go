package schemagen

import (
	"encoding/json"
)

// schema: app.bsky.system.declaration

// RECORDTYPE: SystemDeclaration
type SystemDeclaration struct {
	ActorType string `json:"actorType" cborgen:"actorType"`
}

func (t *SystemDeclaration) MarshalJSON() ([]byte, error) {
	out := make(map[string]interface{})
	out["actorType"] = t.ActorType
	return json.Marshal(out)
}
