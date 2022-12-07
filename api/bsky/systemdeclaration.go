package schemagen

import (
	"encoding/json"
)

// schema: app.bsky.system.declaration

type SystemDeclaration struct {
	ActorType string `json:"actorType"`
}

func (t *SystemDeclaration) MarshalJSON() ([]byte, error) {
	out := make(map[string]interface{})
	out["actorType"] = t.ActorType
	return json.Marshal(out)
}
