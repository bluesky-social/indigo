package schemagen

import (
	"context"

	"github.com/whyrusleeping/gosky/xrpc"
)

// schema: app.bsky.feed.getTimeline

func init() {
}

type FeedGetTimeline_Output struct {
	LexiconTypeID string              `json:"$type,omitempty"`
	Cursor        *string             `json:"cursor" cborgen:"cursor"`
	Feed          []*FeedFeedViewPost `json:"feed" cborgen:"feed"`
}

func FeedGetTimeline(ctx context.Context, c *xrpc.Client, algorithm string, before string, limit int64) (*FeedGetTimeline_Output, error) {
	var out FeedGetTimeline_Output

	params := map[string]interface{}{
		"algorithm": algorithm,
		"before":    before,
		"limit":     limit,
	}
	if err := c.Do(ctx, xrpc.Query, "", "app.bsky.feed.getTimeline", params, nil, &out); err != nil {
		return nil, err
	}

	return &out, nil
}
