package schemagen

import (
	"encoding/json"
)

// schema: app.bsky.system.declRef

type SystemDeclRef struct {
	Cid       string `json:"cid" cborgen:"cid"`
	ActorType string `json:"actorType" cborgen:"actorType"`
}

func (t *SystemDeclRef) MarshalJSON() ([]byte, error) {
	out := make(map[string]interface{})
	out["actorType"] = t.ActorType
	out["cid"] = t.Cid
	return json.Marshal(out)
}
