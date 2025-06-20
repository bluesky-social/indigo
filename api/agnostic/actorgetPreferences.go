// Copied from indigo:api/gndr/actorgetPreferences.go

package agnostic

// schema: gndr.app.actor.getPreferences

import (
	"context"

	"github.com/gander-social/gander-indigo-sovereign/lex/util"
)

// ActorGetPreferences_Output is the output of a gndr.app.actor.getPreferences call.
type ActorGetPreferences_Output struct {
	Preferences []map[string]any `json:"preferences" cborgen:"preferences"`
}

// ActorGetPreferences calls the XRPC method "gndr.app.actor.getPreferences".
func ActorGetPreferences(ctx context.Context, c util.LexClient) (*ActorGetPreferences_Output, error) {
	var out ActorGetPreferences_Output

	params := map[string]interface{}{}
	if err := c.LexDo(ctx, util.Query, "", "gndr.app.actor.getPreferences", params, nil, &out); err != nil {
		return nil, err
	}

	return &out, nil
}
