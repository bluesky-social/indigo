package client

import (
	"fmt"
	"context"
	"encoding/json"

	appbsky "github.com/bluesky-social/indigo/api/bsky"
	"github.com/bluesky-social/indigo/atproto/syntax"
)

func ExampleGet() {

	ctx := context.Background()

	c := APIClient{
		Host: "https://public.api.bsky.app",
	}

	endpoint := syntax.NSID("app.bsky.actor.getProfile")
	params := map[string]string{
		"actor": "atproto.com",
	}
	b, err := c.Get(ctx, endpoint, params)
	if err != nil {
		panic(err)
	}

	var profile appbsky.ActorDefs_ProfileViewDetailed
	if err := json.Unmarshal(*b, &profile); err != nil {
		panic(err)
	}

	fmt.Println(profile.Handle)
	// Output: atproto.com
}
