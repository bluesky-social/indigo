package atproto

import (
	"bytes"
	"context"

	"github.com/bluesky-social/indigo/xrpc"
)

// schema: com.atproto.sync.getBlob

func init() {
}

func SyncGetBlob(ctx context.Context, c *xrpc.Client, cid string, did string) ([]byte, error) {
	buf := new(bytes.Buffer)

	params := map[string]interface{}{
		"cid": cid,
		"did": did,
	}
	if err := c.Do(ctx, xrpc.Query, "", "com.atproto.sync.getBlob", params, nil, buf); err != nil {
		return nil, err
	}

	return buf.Bytes(), nil
}
