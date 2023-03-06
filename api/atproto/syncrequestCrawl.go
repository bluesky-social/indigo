package atproto

import (
	"context"

	"github.com/bluesky-social/indigo/xrpc"
)

// schema: com.atproto.sync.requestCrawl

func init() {
}
func SyncRequestCrawl(ctx context.Context, c *xrpc.Client, host string) error {

	params := map[string]interface{}{
		"host": host,
	}
	if err := c.Do(ctx, xrpc.Query, "", "com.atproto.sync.requestCrawl", params, nil, nil); err != nil {
		return err
	}

	return nil
}
