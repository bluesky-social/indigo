package schemagen

import (
	"context"

	"github.com/whyrusleeping/gosky/xrpc"
)

// schema: app.bsky.actor.getSuggestions

func init() {
}

type ActorGetSuggestions_Actor struct {
	LexiconTypeID string                       `json:"$type,omitempty"`
	Avatar        *string                      `json:"avatar" cborgen:"avatar"`
	Declaration   *SystemDeclRef               `json:"declaration" cborgen:"declaration"`
	Description   *string                      `json:"description" cborgen:"description"`
	Did           string                       `json:"did" cborgen:"did"`
	DisplayName   *string                      `json:"displayName" cborgen:"displayName"`
	Handle        string                       `json:"handle" cborgen:"handle"`
	IndexedAt     *string                      `json:"indexedAt" cborgen:"indexedAt"`
	MyState       *ActorGetSuggestions_MyState `json:"myState" cborgen:"myState"`
}

type ActorGetSuggestions_MyState struct {
	LexiconTypeID string  `json:"$type,omitempty"`
	Follow        *string `json:"follow" cborgen:"follow"`
}

type ActorGetSuggestions_Output struct {
	LexiconTypeID string                       `json:"$type,omitempty"`
	Actors        []*ActorGetSuggestions_Actor `json:"actors" cborgen:"actors"`
	Cursor        *string                      `json:"cursor" cborgen:"cursor"`
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
