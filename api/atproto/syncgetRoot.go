package schemagen

import (
	"context"

	"github.com/bluesky-social/indigo/xrpc"
)

// schema: com.atproto.sync.getRoot

func init() {
}

type SyncGetRoot_Output struct {
	LexiconTypeID string `json:"$type,omitempty"`
	Root          string `json:"root" cborgen:"root"`
}

func SyncGetRoot(ctx context.Context, c *xrpc.Client, did string) (*SyncGetRoot_Output, error) {
	var out SyncGetRoot_Output

	params := map[string]interface{}{
		"did": did,
	}
	if err := c.Do(ctx, xrpc.Query, "", "com.atproto.sync.getRoot", params, nil, &out); err != nil {
		return nil, err
	}

	return &out, nil
}
