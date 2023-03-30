package atproto

import (
	"context"

	"github.com/bluesky-social/indigo/xrpc"
)

// schema: com.atproto.sync.listBlobs

func init() {
}

type SyncListBlobs_Output struct {
	LexiconTypeID string   `json:"$type,omitempty" cborgen:"$type,omitempty"`
	Cids          []string `json:"cids" cborgen:"cids"`
}

func SyncListBlobs(ctx context.Context, c *xrpc.Client, did string, earliest string, latest string) (*SyncListBlobs_Output, error) {
	var out SyncListBlobs_Output

	params := map[string]interface{}{
		"did":      did,
		"earliest": earliest,
		"latest":   latest,
	}
	if err := c.Do(ctx, xrpc.Query, "", "com.atproto.sync.listBlobs", params, nil, &out); err != nil {
		return nil, err
	}

	return &out, nil
}
