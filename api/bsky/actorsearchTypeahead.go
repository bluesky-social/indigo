package bsky

import (
	"context"

	"github.com/bluesky-social/indigo/xrpc"
)

// schema: app.bsky.actor.searchTypeahead

func init() {
}

type ActorSearchTypeahead_Output struct {
	LexiconTypeID string               `json:"$type,omitempty"`
	Users         []*ActorRef_WithInfo `json:"users" cborgen:"users"`
}

func ActorSearchTypeahead(ctx context.Context, c *xrpc.Client, limit int64, term string) (*ActorSearchTypeahead_Output, error) {
	var out ActorSearchTypeahead_Output

	params := map[string]interface{}{
		"limit": limit,
		"term":  term,
	}
	if err := c.Do(ctx, xrpc.Query, "", "app.bsky.actor.searchTypeahead", params, nil, &out); err != nil {
		return nil, err
	}

	return &out, nil
}
