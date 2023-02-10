package bsky

import (
	"context"

	"github.com/bluesky-social/indigo/xrpc"
)

// schema: app.bsky.actor.getProfiles

func init() {
}

type ActorGetProfiles_Output struct {
	LexiconTypeID string               `json:"$type,omitempty"`
	Profiles      []*ActorProfile_View `json:"profiles" cborgen:"profiles"`
}

func ActorGetProfiles(ctx context.Context, c *xrpc.Client, actors []string) (*ActorGetProfiles_Output, error) {
	var out ActorGetProfiles_Output

	params := map[string]interface{}{
		"actors": actors,
	}
	if err := c.Do(ctx, xrpc.Query, "", "app.bsky.actor.getProfiles", params, nil, &out); err != nil {
		return nil, err
	}

	return &out, nil
}
