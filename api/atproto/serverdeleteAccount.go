package atproto

import (
	"context"

	"github.com/bluesky-social/indigo/xrpc"
)

// schema: com.atproto.server.deleteAccount

func init() {
}

type ServerDeleteAccount_Input struct {
	LexiconTypeID string `json:"$type,omitempty" cborgen:"$type,omitempty"`
	Did           string `json:"did" cborgen:"did"`
	Password      string `json:"password" cborgen:"password"`
	Token         string `json:"token" cborgen:"token"`
}

func ServerDeleteAccount(ctx context.Context, c *xrpc.Client, input *ServerDeleteAccount_Input) error {
	if err := c.Do(ctx, xrpc.Procedure, "application/json", "com.atproto.server.deleteAccount", nil, input, nil); err != nil {
		return err
	}

	return nil
}
