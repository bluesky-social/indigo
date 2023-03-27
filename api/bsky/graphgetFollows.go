package bsky

import (
	"context"

	"github.com/bluesky-social/indigo/xrpc"
)

// schema: app.bsky.graph.getFollows

func init() {
}

type GraphGetFollows_Output struct {
	LexiconTypeID string                `json:"$type,omitempty"`
	Cursor        *string               `json:"cursor,omitempty" cborgen:"cursor"`
	Follows       []*ActorDefs_WithInfo `json:"follows" cborgen:"follows"`
	Subject       *ActorDefs_WithInfo   `json:"subject" cborgen:"subject"`
}

func GraphGetFollows(ctx context.Context, c *xrpc.Client, actor string, cursor string, limit int64) (*GraphGetFollows_Output, error) {
	var out GraphGetFollows_Output

	params := map[string]interface{}{
		"actor":  actor,
		"cursor": cursor,
		"limit":  limit,
	}
	if err := c.Do(ctx, xrpc.Query, "", "app.bsky.graph.getFollows", params, nil, &out); err != nil {
		return nil, err
	}

	return &out, nil
}
