package schemagen

import (
	"context"

	"github.com/whyrusleeping/gosky/xrpc"
)

// schema: app.bsky.feed.getAuthorFeed

func init() {
}

type FeedGetAuthorFeed_Output struct {
	LexiconTypeID string              `json:"$type,omitempty"`
	Cursor        *string             `json:"cursor,omitempty" cborgen:"cursor"`
	Feed          []*FeedFeedViewPost `json:"feed" cborgen:"feed"`
}

func FeedGetAuthorFeed(ctx context.Context, c *xrpc.Client, author string, before string, limit int64) (*FeedGetAuthorFeed_Output, error) {
	var out FeedGetAuthorFeed_Output

	params := map[string]interface{}{
		"author": author,
		"before": before,
		"limit":  limit,
	}
	if err := c.Do(ctx, xrpc.Query, "", "app.bsky.feed.getAuthorFeed", params, nil, &out); err != nil {
		return nil, err
	}

	return &out, nil
}
