package bsky

import (
	"context"

	"github.com/bluesky-social/indigo/xrpc"
)

// schema: app.bsky.graph.unmuteActor

func init() {
}

type GraphUnmuteActor_Input struct {
	LexiconTypeID string `json:"$type,omitempty"`
	Actor         string `json:"actor" cborgen:"actor"`
}

func GraphUnmuteActor(ctx context.Context, c *xrpc.Client, input *GraphUnmuteActor_Input) error {
	if err := c.Do(ctx, xrpc.Procedure, "application/json", "app.bsky.graph.unmuteActor", nil, input, nil); err != nil {
		return err
	}

	return nil
}
