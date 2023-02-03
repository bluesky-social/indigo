package atproto

import (
	"context"

	"github.com/bluesky-social/indigo/xrpc"
)

// schema: com.atproto.account.delete

func init() {
}

type AccountDelete_Input struct {
	LexiconTypeID string `json:"$type,omitempty"`
	Did           string `json:"did" cborgen:"did"`
	Password      string `json:"password" cborgen:"password"`
	Token         string `json:"token" cborgen:"token"`
}

func AccountDelete(ctx context.Context, c *xrpc.Client, input *AccountDelete_Input) error {
	if err := c.Do(ctx, xrpc.Procedure, "application/json", "com.atproto.account.delete", nil, input, nil); err != nil {
		return err
	}

	return nil
}
