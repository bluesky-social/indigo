package atproto

import (
	"bytes"
	"context"

	"github.com/bluesky-social/indigo/xrpc"
)

// schema: com.atproto.sync.getCheckout

func init() {
}
func SyncGetCheckout(ctx context.Context, c *xrpc.Client, commit string, did string) ([]byte, error) {
	buf := new(bytes.Buffer)

	params := map[string]interface{}{
		"commit": commit,
		"did":    did,
	}
	if err := c.Do(ctx, xrpc.Query, "", "com.atproto.sync.getCheckout", params, nil, buf); err != nil {
		return nil, err
	}

	return buf.Bytes(), nil
}
