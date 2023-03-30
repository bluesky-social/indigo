package bsky

import (
	"context"

	"github.com/bluesky-social/indigo/xrpc"
)

// schema: app.bsky.graph.getMutes

func init() {
}

type GraphGetMutes_Output struct {
	LexiconTypeID string                   `json:"$type,omitempty" cborgen:"$type,omitempty"`
	Cursor        *string                  `json:"cursor,omitempty" cborgen:"cursor"`
	Mutes         []*ActorDefs_ProfileView `json:"mutes" cborgen:"mutes"`
}

func GraphGetMutes(ctx context.Context, c *xrpc.Client, cursor string, limit int64) (*GraphGetMutes_Output, error) {
	var out GraphGetMutes_Output

	params := map[string]interface{}{
		"cursor": cursor,
		"limit":  limit,
	}
	if err := c.Do(ctx, xrpc.Query, "", "app.bsky.graph.getMutes", params, nil, &out); err != nil {
		return nil, err
	}

	return &out, nil
}
