package schemagen

import (
	"context"

	"github.com/whyrusleeping/gosky/xrpc"
)

// schema: app.bsky.actor.searchTypeahead

func init() {
}

type ActorSearchTypeahead_Output struct {
	LexiconTypeID string                       `json:"$type,omitempty"`
	Users         []*ActorSearchTypeahead_User `json:"users" cborgen:"users"`
}

type ActorSearchTypeahead_User struct {
	LexiconTypeID string         `json:"$type,omitempty"`
	Avatar        *string        `json:"avatar" cborgen:"avatar"`
	Declaration   *SystemDeclRef `json:"declaration" cborgen:"declaration"`
	Did           string         `json:"did" cborgen:"did"`
	DisplayName   *string        `json:"displayName" cborgen:"displayName"`
	Handle        string         `json:"handle" cborgen:"handle"`
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
