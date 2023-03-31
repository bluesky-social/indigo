package bsky

import (
	"context"

	"github.com/bluesky-social/indigo/xrpc"
)

// schema: app.bsky.feed.getTimeline

func init() {
}

type FeedGetTimeline_Output struct {
	LexiconTypeID string                   `json:"$type,omitempty" cborgen:"$type,omitempty"`
	Cursor        *string                  `json:"cursor,omitempty" cborgen:"cursor,omitempty"`
	Feed          []*FeedDefs_FeedViewPost `json:"feed" cborgen:"feed"`
}

func FeedGetTimeline(ctx context.Context, c *xrpc.Client, algorithm string, cursor string, limit int64) (*FeedGetTimeline_Output, error) {
	var out FeedGetTimeline_Output

	params := map[string]interface{}{
		"algorithm": algorithm,
		"cursor":    cursor,
		"limit":     limit,
	}
	if err := c.Do(ctx, xrpc.Query, "", "app.bsky.feed.getTimeline", params, nil, &out); err != nil {
		return nil, err
	}

	return &out, nil
}
