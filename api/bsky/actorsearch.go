package schemagen

import (
	"context"

	"github.com/whyrusleeping/gosky/xrpc"
)

// schema: app.bsky.actor.search

func init() {
}

type ActorSearch_Output struct {
	Cursor *string             `json:"cursor" cborgen:"cursor"`
	Users  []*ActorSearch_User `json:"users" cborgen:"users"`
}

type ActorSearch_User struct {
	Did         string         `json:"did" cborgen:"did"`
	Declaration *SystemDeclRef `json:"declaration" cborgen:"declaration"`
	Handle      string         `json:"handle" cborgen:"handle"`
	DisplayName *string        `json:"displayName" cborgen:"displayName"`
	Description *string        `json:"description" cborgen:"description"`
	IndexedAt   *string        `json:"indexedAt" cborgen:"indexedAt"`
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
