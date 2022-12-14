package schemagen

import (
	"context"

	"github.com/whyrusleeping/gosky/xrpc"
)

// schema: app.bsky.actor.createScene

func init() {
}

type ActorCreateScene_Input struct {
	Handle      string  `json:"handle" cborgen:"handle"`
	RecoveryKey *string `json:"recoveryKey" cborgen:"recoveryKey"`
}

type ActorCreateScene_Output struct {
	Handle      string         `json:"handle" cborgen:"handle"`
	Did         string         `json:"did" cborgen:"did"`
	Declaration *SystemDeclRef `json:"declaration" cborgen:"declaration"`
}

func ActorCreateScene(ctx context.Context, c *xrpc.Client, input ActorCreateScene_Input) (*ActorCreateScene_Output, error) {
	var out ActorCreateScene_Output
	if err := c.Do(ctx, xrpc.Procedure, "application/json", "app.bsky.actor.createScene", nil, input, &out); err != nil {
		return nil, err
	}

	return &out, nil
}
