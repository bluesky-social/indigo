package bsky

import (
	"context"

	"github.com/bluesky-social/indigo/xrpc"
)

// schema: app.bsky.actor.search

func init() {
}

type ActorSearch_Output struct {
	LexiconTypeID string                    `json:"$type,omitempty"`
	Cursor        *string                   `json:"cursor,omitempty" cborgen:"cursor"`
	Users         []*ActorProfile_ViewBasic `json:"users" cborgen:"users"`
}

func ActorSearch(ctx context.Context, c *xrpc.Client, before string, limit int64, term string) (*ActorSearch_Output, error) {
	var out ActorSearch_Output

	params := map[string]interface{}{
		"before": before,
		"limit":  limit,
		"term":   term,
	}
	if err := c.Do(ctx, xrpc.Query, "", "app.bsky.actor.search", params, nil, &out); err != nil {
		return nil, err
	}

	return &out, nil
}
