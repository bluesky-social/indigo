package bsky

import (
	"context"

	"github.com/bluesky-social/indigo/xrpc"
)

// schema: app.bsky.graph.getFollowers

func init() {
}

type GraphGetFollowers_Output struct {
	LexiconTypeID string                   `json:"$type,omitempty"`
	Cursor        *string                  `json:"cursor,omitempty" cborgen:"cursor"`
	Followers     []*ActorDefs_ProfileView `json:"followers" cborgen:"followers"`
	Subject       *ActorDefs_ProfileView   `json:"subject" cborgen:"subject"`
}

func GraphGetFollowers(ctx context.Context, c *xrpc.Client, actor string, cursor string, limit int64) (*GraphGetFollowers_Output, error) {
	var out GraphGetFollowers_Output

	params := map[string]interface{}{
		"actor":  actor,
		"cursor": cursor,
		"limit":  limit,
	}
	if err := c.Do(ctx, xrpc.Query, "", "app.bsky.graph.getFollowers", params, nil, &out); err != nil {
		return nil, err
	}

	return &out, nil
}
