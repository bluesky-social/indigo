package atproto

import (
	"context"

	"github.com/bluesky-social/indigo/xrpc"
)

// schema: com.atproto.peering.init

func init() {
}

type PeeringInit_Input struct {
	LexiconTypeID string `json:"$type,omitempty"`
	Pds           string `json:"pds" cborgen:"pds"`
}

func PeeringInit(ctx context.Context, c *xrpc.Client, input *PeeringInit_Input) error {
	if err := c.Do(ctx, xrpc.Procedure, "application/json", "com.atproto.peering.init", nil, input, nil); err != nil {
		return err
	}

	return nil
}
