package atproto

import (
	"context"

	"github.com/bluesky-social/indigo/xrpc"
)

// schema: com.atproto.identity.updateHandle

func init() {
}

type IdentityUpdateHandle_Input struct {
	Handle string `json:"handle" cborgen:"handle"`
}

func IdentityUpdateHandle(ctx context.Context, c *xrpc.Client, input *IdentityUpdateHandle_Input) error {
	if err := c.Do(ctx, xrpc.Procedure, "application/json", "com.atproto.identity.updateHandle", nil, input, nil); err != nil {
		return err
	}

	return nil
}
