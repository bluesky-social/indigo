package schemagen

import (
	"context"

	"github.com/whyrusleeping/gosky/xrpc"
)

// schema: app.bsky.actor.searchTypeahead

func init() {
}

type ActorSearchTypeahead_User struct {
	DisplayName *string        `json:"displayName" cborgen:"displayName"`
	Did         string         `json:"did" cborgen:"did"`
	Declaration *SystemDeclRef `json:"declaration" cborgen:"declaration"`
	Handle      string         `json:"handle" cborgen:"handle"`
}

type ActorSearchTypeahead_Output struct {
	Users []*ActorSearchTypeahead_User `json:"users" cborgen:"users"`
}

func ActorSearchTypeahead(ctx context.Context, c *xrpc.Client, limit int64, term string) (*ActorSearchTypeahead_Output, error) {
	var out ActorSearchTypeahead_Output

	params := map[string]interface{}{
		"limit": limit,
		"term":  term,
	}
	if err := c.Do(ctx, xrpc.Query, "", "app.bsky.actor.searchTypeahead", params, nil, &out); err != nil {
		return nil, err
	}

	return &out, nil
}
