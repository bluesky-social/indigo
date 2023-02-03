package bsky

import (
	"context"

	"github.com/bluesky-social/indigo/xrpc"
)

// schema: app.bsky.actor.search

func init() {
}

type ActorSearch_Output struct {
	LexiconTypeID string              `json:"$type,omitempty"`
	Cursor        *string             `json:"cursor,omitempty" cborgen:"cursor"`
	Users         []*ActorSearch_User `json:"users" cborgen:"users"`
}

type ActorSearch_User struct {
	LexiconTypeID string         `json:"$type,omitempty"`
	Avatar        *string        `json:"avatar,omitempty" cborgen:"avatar"`
	Declaration   *SystemDeclRef `json:"declaration" cborgen:"declaration"`
	Description   *string        `json:"description,omitempty" cborgen:"description"`
	Did           string         `json:"did" cborgen:"did"`
	DisplayName   *string        `json:"displayName,omitempty" cborgen:"displayName"`
	Handle        string         `json:"handle" cborgen:"handle"`
	IndexedAt     *string        `json:"indexedAt,omitempty" cborgen:"indexedAt"`
}

func ActorSearch(ctx context.Context, c *xrpc.Client, before string, limit int64, term string) (*ActorSearch_Output, error) {
	var out ActorSearch_Output

	params := map[string]interface{}{
		"before": before,
		"limit":  limit,
		"term":   term,
	}
	if err := c.Do(ctx, xrpc.Query, "", "app.bsky.actor.search", params, nil, &out); err != nil {
		return nil, err
	}

	return &out, nil
}
