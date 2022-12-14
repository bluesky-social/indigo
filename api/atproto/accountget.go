package schemagen

import (
	"context"

	"github.com/whyrusleeping/gosky/xrpc"
)

// schema: com.atproto.account.get

func init() {
}
func AccountGet(ctx context.Context, c *xrpc.Client) error {
	if err := c.Do(ctx, xrpc.Query, "", "com.atproto.account.get", nil, nil, nil); err != nil {
		return err
	}

	return nil
}
