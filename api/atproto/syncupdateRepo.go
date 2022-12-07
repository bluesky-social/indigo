package schemagen

import (
	"context"
	"io"

	"github.com/whyrusleeping/gosky/xrpc"
)

// schema: com.atproto.sync.updateRepo

func SyncUpdateRepo(ctx context.Context, c *xrpc.Client, input io.Reader, did string) error {

	params := map[string]interface{}{
		"did": did,
	}
	if err := c.Do(ctx, xrpc.Procedure, "application/cbor", "com.atproto.sync.updateRepo", params, input, nil); err != nil {
		return err
	}

	return nil
}
