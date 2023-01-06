package schemagen

import (
	"context"

	"github.com/whyrusleeping/gosky/lex/util"
	"github.com/whyrusleeping/gosky/xrpc"
)

// schema: app.bsky.actor.updateProfile

func init() {
}

type ActorUpdateProfile_Input struct {
	LexiconTypeID string     `json:"$type,omitempty"`
	Avatar        *util.Blob `json:"avatar" cborgen:"avatar"`
	Banner        *util.Blob `json:"banner" cborgen:"banner"`
	Description   *string    `json:"description" cborgen:"description"`
	Did           *string    `json:"did" cborgen:"did"`
	DisplayName   *string    `json:"displayName" cborgen:"displayName"`
}

type ActorUpdateProfile_Output struct {
	LexiconTypeID string `json:"$type,omitempty"`
	Cid           string `json:"cid" cborgen:"cid"`
	Record        any    `json:"record" cborgen:"record"`
	Uri           string `json:"uri" cborgen:"uri"`
}

func ActorUpdateProfile(ctx context.Context, c *xrpc.Client, input *ActorUpdateProfile_Input) (*ActorUpdateProfile_Output, error) {
	var out ActorUpdateProfile_Output
	if err := c.Do(ctx, xrpc.Procedure, "application/json", "app.bsky.actor.updateProfile", nil, input, &out); err != nil {
		return nil, err
	}

	return &out, nil
}
