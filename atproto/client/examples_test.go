package client

import (
	"context"
	"fmt"

	appgndr "github.com/gander-social/gander-indigo-sovereign/api/gndr"
	"github.com/gander-social/gander-indigo-sovereign/atproto/syntax"
)

func ExampleGet() {

	ctx := context.Background()

	c := APIClient{
		Host: "https://public.api.gndr.app",
	}

	endpoint := syntax.NSID("gndr.app.actor.getProfile")
	params := map[string]any{
		"actor": "atproto.com",
	}
	var profile appgndr.ActorDefs_ProfileViewDetailed
	if err := c.Get(ctx, endpoint, params, &profile); err != nil {
		panic(err)
	}

	fmt.Println(profile.Handle)
	// Output: atproto.com
}
