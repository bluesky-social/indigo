package atproto

import (
	"context"

	"github.com/bluesky-social/indigo/xrpc"
)

// schema: com.atproto.server.revokeAppPassword

func init() {
}

type ServerRevokeAppPassword_Input struct {
	Name string `json:"name" cborgen:"name"`
}

func ServerRevokeAppPassword(ctx context.Context, c *xrpc.Client, input *ServerRevokeAppPassword_Input) error {
	if err := c.Do(ctx, xrpc.Procedure, "application/json", "com.atproto.server.revokeAppPassword", nil, input, nil); err != nil {
		return err
	}

	return nil
}
