package atproto

import (
	"context"

	"github.com/bluesky-social/indigo/xrpc"
)

// schema: com.atproto.sync.notifyOfUpdate

func init() {
}
func SyncNotifyOfUpdate(ctx context.Context, c *xrpc.Client, hostname string) error {

	params := map[string]interface{}{
		"hostname": hostname,
	}
	if err := c.Do(ctx, xrpc.Query, "", "com.atproto.sync.notifyOfUpdate", params, nil, nil); err != nil {
		return err
	}

	return nil
}
