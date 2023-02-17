package bsky

import (
	"context"

	"github.com/bluesky-social/indigo/xrpc"
)

// schema: app.bsky.actor.getProfile

func init() {
}
func ActorGetProfile(ctx context.Context, c *xrpc.Client, actor string) (*ActorProfile_View, error) {
	var out ActorProfile_View

	params := map[string]interface{}{
		"actor": actor,
	}
	if err := c.Do(ctx, xrpc.Query, "", "app.bsky.actor.getProfile", params, nil, &out); err != nil {
		return nil, err
	}

	return &out, nil
}
