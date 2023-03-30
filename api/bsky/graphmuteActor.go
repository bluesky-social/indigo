package bsky

import (
	"context"

	"github.com/bluesky-social/indigo/xrpc"
)

// schema: app.bsky.graph.muteActor

func init() {
}

type GraphMuteActor_Input struct {
	LexiconTypeID string `json:"$type,omitempty" cborgen:"$type,omitempty"`
	Actor         string `json:"actor" cborgen:"actor"`
}

func GraphMuteActor(ctx context.Context, c *xrpc.Client, input *GraphMuteActor_Input) error {
	if err := c.Do(ctx, xrpc.Procedure, "application/json", "app.bsky.graph.muteActor", nil, input, nil); err != nil {
		return err
	}

	return nil
}
