package schemagen

import (
	"encoding/json"
)

// schema: app.bsky.actor.ref

type ActorRef struct {
	Did            string `json:"did" cborgen:"did"`
	DeclarationCid string `json:"declarationCid" cborgen:"declarationCid"`
}

func (t *ActorRef) MarshalJSON() ([]byte, error) {
	out := make(map[string]interface{})
	out["declarationCid"] = t.DeclarationCid
	out["did"] = t.Did
	return json.Marshal(out)
}

type ActorRef_WithInfo struct {
	Did         string         `json:"did" cborgen:"did"`
	Declaration *SystemDeclRef `json:"declaration" cborgen:"declaration"`
	Handle      string         `json:"handle" cborgen:"handle"`
	DisplayName string         `json:"displayName" cborgen:"displayName"`
}

func (t *ActorRef_WithInfo) MarshalJSON() ([]byte, error) {
	out := make(map[string]interface{})
	out["declaration"] = t.Declaration
	out["did"] = t.Did
	out["displayName"] = t.DisplayName
	out["handle"] = t.Handle
	return json.Marshal(out)
}
