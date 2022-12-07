package schemagen

import (
	"encoding/json"
)

// schema: app.bsky.actor.ref

type ActorRef struct {
	Did            string `json:"did"`
	DeclarationCid string `json:"declarationCid"`
}

func (t *ActorRef) MarshalJSON() ([]byte, error) {
	out := make(map[string]interface{})
	out["declarationCid"] = t.DeclarationCid
	out["did"] = t.Did
	return json.Marshal(out)
}

type ActorRef_WithInfo struct {
	Did         string         `json:"did"`
	Declaration *SystemDeclRef `json:"declaration"`
	Handle      string         `json:"handle"`
	DisplayName string         `json:"displayName"`
}

func (t *ActorRef_WithInfo) MarshalJSON() ([]byte, error) {
	out := make(map[string]interface{})
	out["declaration"] = t.Declaration
	out["did"] = t.Did
	out["displayName"] = t.DisplayName
	out["handle"] = t.Handle
	return json.Marshal(out)
}
