package atproto

import (
	"context"

	"github.com/bluesky-social/indigo/xrpc"
)

// schema: com.atproto.session.delete

func init() {
}
func SessionDelete(ctx context.Context, c *xrpc.Client) error {
	if err := c.Do(ctx, xrpc.Procedure, "", "com.atproto.session.delete", nil, nil, nil); err != nil {
		return err
	}

	return nil
}
