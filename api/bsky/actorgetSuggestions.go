package schemagen

import (
	"context"

	"github.com/whyrusleeping/gosky/xrpc"
)

// schema: app.bsky.actor.getSuggestions

func init() {
}

type ActorGetSuggestions_Output struct {
	Cursor *string                      `json:"cursor" cborgen:"cursor"`
	Actors []*ActorGetSuggestions_Actor `json:"actors" cborgen:"actors"`
}

type ActorGetSuggestions_Actor struct {
	MyState     *ActorGetSuggestions_MyState `json:"myState" cborgen:"myState"`
	Did         string                       `json:"did" cborgen:"did"`
	Declaration *SystemDeclRef               `json:"declaration" cborgen:"declaration"`
	Handle      string                       `json:"handle" cborgen:"handle"`
	DisplayName *string                      `json:"displayName" cborgen:"displayName"`
	Description *string                      `json:"description" cborgen:"description"`
	IndexedAt   *string                      `json:"indexedAt" cborgen:"indexedAt"`
}

type ActorGetSuggestions_MyState struct {
	Follow string `json:"follow" cborgen:"follow"`
}

func ActorGetSuggestions(ctx context.Context, c *xrpc.Client, cursor string, limit int64) (*ActorGetSuggestions_Output, error) {
	var out ActorGetSuggestions_Output

	params := map[string]interface{}{
		"cursor": cursor,
		"limit":  limit,
	}
	if err := c.Do(ctx, xrpc.Query, "", "app.bsky.actor.getSuggestions", params, nil, &out); err != nil {
		return nil, err
	}

	return &out, nil
}
