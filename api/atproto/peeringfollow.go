package schemagen

import (
	"context"

	"github.com/whyrusleeping/gosky/xrpc"
)

// schema: com.atproto.peering.follow

func init() {
}

type PeeringFollow_Input struct {
	LexiconTypeID string   `json:"$type,omitempty"`
	Users         []string `json:"users" cborgen:"users"`
}

func PeeringFollow(ctx context.Context, c *xrpc.Client, input *PeeringFollow_Input) error {
	if err := c.Do(ctx, xrpc.Procedure, "application/json", "com.atproto.peering.follow", nil, input, nil); err != nil {
		return err
	}

	return nil
}
