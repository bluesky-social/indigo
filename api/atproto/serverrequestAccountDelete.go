package atproto

import (
	"context"

	"github.com/bluesky-social/indigo/xrpc"
)

// schema: com.atproto.server.requestAccountDelete

func init() {
}
func ServerRequestAccountDelete(ctx context.Context, c *xrpc.Client) error {
	if err := c.Do(ctx, xrpc.Procedure, "", "com.atproto.server.requestAccountDelete", nil, nil, nil); err != nil {
		return err
	}

	return nil
}
