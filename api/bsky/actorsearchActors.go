package bsky

import (
	"context"

	"github.com/bluesky-social/indigo/xrpc"
)

// schema: app.bsky.actor.searchActors

func init() {
}

type ActorSearchActors_Output struct {
	Actors []*ActorDefs_ProfileView `json:"actors" cborgen:"actors"`
	Cursor *string                  `json:"cursor,omitempty" cborgen:"cursor,omitempty"`
}

func ActorSearchActors(ctx context.Context, c *xrpc.Client, cursor string, limit int64, term string) (*ActorSearchActors_Output, error) {
	var out ActorSearchActors_Output

	params := map[string]interface{}{
		"cursor": cursor,
		"limit":  limit,
		"term":   term,
	}
	if err := c.Do(ctx, xrpc.Query, "", "app.bsky.actor.searchActors", params, nil, &out); err != nil {
		return nil, err
	}

	return &out, nil
}
