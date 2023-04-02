package atproto

import (
	"context"

	"github.com/bluesky-social/indigo/xrpc"
)

// schema: com.atproto.server.resetPassword

func init() {
}

type ServerResetPassword_Input struct {
	LexiconTypeID string `json:"$type,omitempty" cborgen:"$type,omitempty"`
	Password      string `json:"password" cborgen:"password"`
	Token         string `json:"token" cborgen:"token"`
}

func ServerResetPassword(ctx context.Context, c *xrpc.Client, input *ServerResetPassword_Input) error {
	if err := c.Do(ctx, xrpc.Procedure, "application/json", "com.atproto.server.resetPassword", nil, input, nil); err != nil {
		return err
	}

	return nil
}
