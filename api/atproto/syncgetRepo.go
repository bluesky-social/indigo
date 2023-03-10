package atproto

import (
	"bytes"
	"context"

	"github.com/bluesky-social/indigo/xrpc"
)

// schema: com.atproto.sync.getRepo

func init() {
}
func SyncGetRepo(ctx context.Context, c *xrpc.Client, did string, earliest string, latest string) ([]byte, error) {
	buf := new(bytes.Buffer)

	params := map[string]interface{}{
		"did":      did,
		"earliest": earliest,
		"latest":   latest,
	}
	if err := c.Do(ctx, xrpc.Query, "", "com.atproto.sync.getRepo", params, nil, buf); err != nil {
		return nil, err
	}

	return buf.Bytes(), nil
}
