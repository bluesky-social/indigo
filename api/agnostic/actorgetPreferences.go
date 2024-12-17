// Copied from indigo:api/bsky/actorgetPreferences.go

package agnostic

// schema: app.bsky.actor.getPreferences

import (
	"context"

	"github.com/bluesky-social/indigo/xrpc"
)

// ActorGetPreferences_Output is the output of a app.bsky.actor.getPreferences call.
type ActorGetPreferences_Output struct {
	Preferences []map[string]any `json:"preferences" cborgen:"preferences"`
}

// ActorGetPreferences calls the XRPC method "app.bsky.actor.getPreferences".
func ActorGetPreferences(ctx context.Context, c *xrpc.Client) (*ActorGetPreferences_Output, error) {
	var out ActorGetPreferences_Output

	params := map[string]interface{}{}
	if err := c.Do(ctx, xrpc.Query, "", "app.bsky.actor.getPreferences", params, nil, &out); err != nil {
		return nil, err
	}

	return &out, nil
}
