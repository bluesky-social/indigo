package schemagen

import (
	"context"
	"encoding/json"

	"github.com/whyrusleeping/gosky/xrpc"
)

// schema: app.bsky.actor.updateProfile

type ActorUpdateProfile_Input struct {
	Did         string `json:"did" cborgen:"did"`
	DisplayName string `json:"displayName" cborgen:"displayName"`
	Description string `json:"description" cborgen:"description"`
}

func (t *ActorUpdateProfile_Input) MarshalJSON() ([]byte, error) {
	out := make(map[string]interface{})
	out["description"] = t.Description
	out["did"] = t.Did
	out["displayName"] = t.DisplayName
	return json.Marshal(out)
}

type ActorUpdateProfile_Output struct {
	Uri    string `json:"uri" cborgen:"uri"`
	Cid    string `json:"cid" cborgen:"cid"`
	Record any    `json:"record" cborgen:"record"`
}

func (t *ActorUpdateProfile_Output) MarshalJSON() ([]byte, error) {
	out := make(map[string]interface{})
	out["cid"] = t.Cid
	out["record"] = t.Record
	out["uri"] = t.Uri
	return json.Marshal(out)
}

func ActorUpdateProfile(ctx context.Context, c *xrpc.Client, input ActorUpdateProfile_Input) (*ActorUpdateProfile_Output, error) {
	var out ActorUpdateProfile_Output
	if err := c.Do(ctx, xrpc.Procedure, "application/json", "app.bsky.actor.updateProfile", nil, input, &out); err != nil {
		return nil, err
	}

	return &out, nil
}
