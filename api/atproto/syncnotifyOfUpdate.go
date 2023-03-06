package atproto

import (
	"context"

	"github.com/bluesky-social/indigo/xrpc"
)

// schema: com.atproto.sync.notifyOfUpdate

func init() {
}
func SyncNotifyOfUpdate(ctx context.Context, c *xrpc.Client) error {
	if err := c.Do(ctx, xrpc.Query, "", "com.atproto.sync.notifyOfUpdate", nil, nil, nil); err != nil {
		return err
	}

	return nil
}
