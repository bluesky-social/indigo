package schemagen

import (
	"context"

	"github.com/whyrusleeping/gosky/xrpc"
)

// schema: app.bsky.actor.updateProfile

func init() {
}

type ActorUpdateProfile_Input struct {
	DisplayName string `json:"displayName" cborgen:"displayName"`
	Description string `json:"description" cborgen:"description"`
	Did         string `json:"did" cborgen:"did"`
}

type ActorUpdateProfile_Output struct {
	Uri    string `json:"uri" cborgen:"uri"`
	Cid    string `json:"cid" cborgen:"cid"`
	Record any    `json:"record" cborgen:"record"`
}

func ActorUpdateProfile(ctx context.Context, c *xrpc.Client, input ActorUpdateProfile_Input) (*ActorUpdateProfile_Output, error) {
	var out ActorUpdateProfile_Output
	if err := c.Do(ctx, xrpc.Procedure, "application/json", "app.bsky.actor.updateProfile", nil, input, &out); err != nil {
		return nil, err
	}

	return &out, nil
}
