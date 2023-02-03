package bsky

import (
	"context"

	"github.com/bluesky-social/indigo/xrpc"
)

// schema: app.bsky.graph.mute

func init() {
}

type GraphMute_Input struct {
	LexiconTypeID string `json:"$type,omitempty"`
	User          string `json:"user" cborgen:"user"`
}

func GraphMute(ctx context.Context, c *xrpc.Client, input *GraphMute_Input) error {
	if err := c.Do(ctx, xrpc.Procedure, "application/json", "app.bsky.graph.mute", nil, input, nil); err != nil {
		return err
	}

	return nil
}
