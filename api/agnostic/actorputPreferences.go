// Copied from indigo:api/gndr/actorputPreferences.go

package agnostic

// schema: gndr.app.actor.putPreferences

import (
	"context"

	"github.com/gander-social/gander-indigo-sovereign/lex/util"
)

// ActorPutPreferences_Input is the input argument to a gndr.app.actor.putPreferences call.
type ActorPutPreferences_Input struct {
	Preferences []map[string]any `json:"preferences" cborgen:"preferences"`
}

// ActorPutPreferences calls the XRPC method "gndr.app.actor.putPreferences".
func ActorPutPreferences(ctx context.Context, c util.LexClient, input *ActorPutPreferences_Input) error {
	if err := c.LexDo(ctx, util.Procedure, "application/json", "gndr.app.actor.putPreferences", nil, input, nil); err != nil {
		return err
	}

	return nil
}
