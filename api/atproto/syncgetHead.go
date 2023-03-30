package atproto

import (
	"context"

	"github.com/bluesky-social/indigo/xrpc"
)

// schema: com.atproto.sync.getHead

func init() {
}

type SyncGetHead_Output struct {
	LexiconTypeID string `json:"$type,omitempty" cborgen:"$type,omitempty"`
	Root          string `json:"root" cborgen:"root"`
}

func SyncGetHead(ctx context.Context, c *xrpc.Client, did string) (*SyncGetHead_Output, error) {
	var out SyncGetHead_Output

	params := map[string]interface{}{
		"did": did,
	}
	if err := c.Do(ctx, xrpc.Query, "", "com.atproto.sync.getHead", params, nil, &out); err != nil {
		return nil, err
	}

	return &out, nil
}
