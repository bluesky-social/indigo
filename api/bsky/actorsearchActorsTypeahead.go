package bsky

import (
	"context"

	"github.com/bluesky-social/indigo/xrpc"
)

// schema: app.bsky.actor.searchActorsTypeahead

func init() {
}

type ActorSearchActorsTypeahead_Output struct {
	LexiconTypeID string                        `json:"$type,omitempty" cborgen:"$type,omitempty"`
	Actors        []*ActorDefs_ProfileViewBasic `json:"actors" cborgen:"actors"`
}

func ActorSearchActorsTypeahead(ctx context.Context, c *xrpc.Client, limit int64, term string) (*ActorSearchActorsTypeahead_Output, error) {
	var out ActorSearchActorsTypeahead_Output

	params := map[string]interface{}{
		"limit": limit,
		"term":  term,
	}
	if err := c.Do(ctx, xrpc.Query, "", "app.bsky.actor.searchActorsTypeahead", params, nil, &out); err != nil {
		return nil, err
	}

	return &out, nil
}
