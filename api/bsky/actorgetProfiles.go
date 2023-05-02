// Code generated by cmd/lexgen (see Makefile's lexgen); DO NOT EDIT.

package bsky

// schema: app.bsky.actor.getProfiles

import (
	"context"

	"github.com/bluesky-social/indigo/lex/util"
	"github.com/bluesky-social/indigo/xrpc"
)

// ActorGetProfiles_Output is the output of a app.bsky.actor.getProfiles call.
type ActorGetProfiles_Output struct {
	Profiles []*ActorDefs_ProfileViewDetailed `json:"profiles" cborgen:"profiles"`
}

// ActorGetProfiles calls the XRPC method "app.bsky.actor.getProfiles".
func ActorGetProfiles(ctx context.Context, c *xrpc.Client, actors []util.FormatAtIdentifier) (*ActorGetProfiles_Output, error) {
	var out ActorGetProfiles_Output

	params := map[string]interface{}{
		"actors": actors,
	}
	if err := c.Do(ctx, xrpc.Query, "", "app.bsky.actor.getProfiles", params, nil, &out); err != nil {
		return nil, err
	}

	return &out, nil
}
