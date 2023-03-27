package bsky

import (
	"context"

	"github.com/bluesky-social/indigo/xrpc"
)

// schema: app.bsky.unspecced.getPopular

func init() {
}

type UnspeccedGetPopular_Output struct {
	LexiconTypeID string                   `json:"$type,omitempty"`
	Cursor        *string                  `json:"cursor,omitempty" cborgen:"cursor"`
	Feed          []*FeedDefs_FeedViewPost `json:"feed" cborgen:"feed"`
}

func UnspeccedGetPopular(ctx context.Context, c *xrpc.Client, cursor string, limit int64) (*UnspeccedGetPopular_Output, error) {
	var out UnspeccedGetPopular_Output

	params := map[string]interface{}{
		"cursor": cursor,
		"limit":  limit,
	}
	if err := c.Do(ctx, xrpc.Query, "", "app.bsky.unspecced.getPopular", params, nil, &out); err != nil {
		return nil, err
	}

	return &out, nil
}
