package bsky

import (
	"context"

	"github.com/bluesky-social/indigo/xrpc"
)

// schema: app.bsky.actor.getSuggestions

func init() {
}

type ActorGetSuggestions_Actor struct {
	LexiconTypeID string         `json:"$type,omitempty"`
	Avatar        *string        `json:"avatar,omitempty" cborgen:"avatar"`
	Declaration   *SystemDeclRef `json:"declaration" cborgen:"declaration"`
	Description   *string        `json:"description,omitempty" cborgen:"description"`
	Did           string         `json:"did" cborgen:"did"`
	DisplayName   *string        `json:"displayName,omitempty" cborgen:"displayName"`
	Handle        string         `json:"handle" cborgen:"handle"`
	IndexedAt     *string        `json:"indexedAt,omitempty" cborgen:"indexedAt"`
}

type ActorGetSuggestions_Output struct {
	LexiconTypeID string                       `json:"$type,omitempty"`
	Actors        []*ActorGetSuggestions_Actor `json:"actors" cborgen:"actors"`
	Cursor        *string                      `json:"cursor,omitempty" cborgen:"cursor"`
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
