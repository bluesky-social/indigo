package atproto

import (
	"context"

	"github.com/bluesky-social/indigo/xrpc"
)

// schema: com.atproto.account.requestDelete

func init() {
}
func AccountRequestDelete(ctx context.Context, c *xrpc.Client) error {
	if err := c.Do(ctx, xrpc.Procedure, "", "com.atproto.account.requestDelete", nil, nil, nil); err != nil {
		return err
	}

	return nil
}
