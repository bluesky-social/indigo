package schemagen

import (
	"context"

	"github.com/bluesky-social/indigo/xrpc"
)

// schema: app.bsky.actor.createScene

func init() {
}

type ActorCreateScene_Input struct {
	LexiconTypeID string  `json:"$type,omitempty"`
	Handle        string  `json:"handle" cborgen:"handle"`
	RecoveryKey   *string `json:"recoveryKey,omitempty" cborgen:"recoveryKey"`
}

type ActorCreateScene_Output struct {
	LexiconTypeID string         `json:"$type,omitempty"`
	Declaration   *SystemDeclRef `json:"declaration" cborgen:"declaration"`
	Did           string         `json:"did" cborgen:"did"`
	Handle        string         `json:"handle" cborgen:"handle"`
}

func ActorCreateScene(ctx context.Context, c *xrpc.Client, input *ActorCreateScene_Input) (*ActorCreateScene_Output, error) {
	var out ActorCreateScene_Output
	if err := c.Do(ctx, xrpc.Procedure, "application/json", "app.bsky.actor.createScene", nil, input, &out); err != nil {
		return nil, err
	}

	return &out, nil
}
