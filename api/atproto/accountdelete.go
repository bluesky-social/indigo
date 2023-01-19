package schemagen

import (
	"context"

	"github.com/bluesky-social/indigo/xrpc"
)

// schema: com.atproto.account.delete

func init() {
}
func AccountDelete(ctx context.Context, c *xrpc.Client) error {
	if err := c.Do(ctx, xrpc.Procedure, "", "com.atproto.account.delete", nil, nil, nil); err != nil {
		return err
	}

	return nil
}
