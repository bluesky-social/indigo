package atproto

import (
	"bytes"
	"context"

	"github.com/bluesky-social/indigo/xrpc"
)

// schema: com.atproto.sync.getBlocks

func init() {
}
func SyncGetBlocks(ctx context.Context, c *xrpc.Client, cids []string, did string) ([]byte, error) {
	buf := new(bytes.Buffer)

	params := map[string]interface{}{
		"cids": cids,
		"did":  did,
	}
	if err := c.Do(ctx, xrpc.Query, "", "com.atproto.sync.getBlocks", params, nil, buf); err != nil {
		return nil, err
	}

	return buf.Bytes(), nil
}
