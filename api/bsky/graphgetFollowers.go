package bsky

import (
	"context"

	"github.com/bluesky-social/indigo/xrpc"
)

// schema: app.bsky.graph.getFollowers

func init() {
}

type GraphGetFollowers_Output struct {
	LexiconTypeID string               `json:"$type,omitempty"`
	Cursor        *string              `json:"cursor,omitempty" cborgen:"cursor"`
	Followers     []*ActorRef_WithInfo `json:"followers" cborgen:"followers"`
	Subject       *ActorRef_WithInfo   `json:"subject" cborgen:"subject"`
}

func GraphGetFollowers(ctx context.Context, c *xrpc.Client, before string, limit int64, user string) (*GraphGetFollowers_Output, error) {
	var out GraphGetFollowers_Output

	params := map[string]interface{}{
		"before": before,
		"limit":  limit,
		"user":   user,
	}
	if err := c.Do(ctx, xrpc.Query, "", "app.bsky.graph.getFollowers", params, nil, &out); err != nil {
		return nil, err
	}

	return &out, nil
}
