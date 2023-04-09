package atproto

import (
	"context"

	"github.com/bluesky-social/indigo/xrpc"
)

// schema: com.atproto.server.requestPasswordReset

func init() {
}

type ServerRequestPasswordReset_Input struct {
	Email string `json:"email" cborgen:"email"`
}

func ServerRequestPasswordReset(ctx context.Context, c *xrpc.Client, input *ServerRequestPasswordReset_Input) error {
	if err := c.Do(ctx, xrpc.Procedure, "application/json", "com.atproto.server.requestPasswordReset", nil, input, nil); err != nil {
		return err
	}

	return nil
}
