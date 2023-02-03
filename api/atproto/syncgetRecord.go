package atproto

import (
	"bytes"
	"context"

	"github.com/bluesky-social/indigo/xrpc"
)

// schema: com.atproto.sync.getRecord

func init() {
}
func SyncGetRecord(ctx context.Context, c *xrpc.Client, collection string, commit string, did string, rkey string) ([]byte, error) {
	buf := new(bytes.Buffer)

	params := map[string]interface{}{
		"collection": collection,
		"commit":     commit,
		"did":        did,
		"rkey":       rkey,
	}
	if err := c.Do(ctx, xrpc.Query, "", "com.atproto.sync.getRecord", params, nil, buf); err != nil {
		return nil, err
	}

	return buf.Bytes(), nil
}
