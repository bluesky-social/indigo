package client

import (
	"context"
	"fmt"

	appbsky "github.com/bluesky-social/indigo/api/bsky"
	"github.com/bluesky-social/indigo/atproto/syntax"
)

func ExampleGet() {

	ctx := context.Background()

	c := APIClient{
		Host: "https://public.api.bsky.app",
	}

	endpoint := syntax.NSID("app.bsky.actor.getProfile")
	params := map[string]any{
		"actor": "atproto.com",
	}
	var profile appbsky.ActorDefs_ProfileViewDetailed
	if err := c.Get(ctx, endpoint, params, &profile); err != nil {
		panic(err)
	}

	fmt.Println(profile.Handle)
	// Output: atproto.com
}
