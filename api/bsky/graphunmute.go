package schemagen

import (
	"context"

	"github.com/bluesky-social/indigo/xrpc"
)

// schema: app.bsky.graph.unmute

func init() {
}

type GraphUnmute_Input struct {
	LexiconTypeID string `json:"$type,omitempty"`
	User          string `json:"user" cborgen:"user"`
}

func GraphUnmute(ctx context.Context, c *xrpc.Client, input *GraphUnmute_Input) error {
	if err := c.Do(ctx, xrpc.Procedure, "application/json", "app.bsky.graph.unmute", nil, input, nil); err != nil {
		return err
	}

	return nil
}
