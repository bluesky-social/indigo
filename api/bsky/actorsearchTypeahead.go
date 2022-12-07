package schemagen

import (
	"context"
	"encoding/json"

	"github.com/whyrusleeping/gosky/xrpc"
)

// schema: app.bsky.actor.searchTypeahead

type ActorSearchTypeahead_Output struct {
	Users []*ActorSearchTypeahead_User `json:"users"`
}

func (t *ActorSearchTypeahead_Output) MarshalJSON() ([]byte, error) {
	out := make(map[string]interface{})
	out["users"] = t.Users
	return json.Marshal(out)
}

type ActorSearchTypeahead_User struct {
	Did         string         `json:"did"`
	Declaration *SystemDeclRef `json:"declaration"`
	Handle      string         `json:"handle"`
	DisplayName string         `json:"displayName"`
}

func (t *ActorSearchTypeahead_User) MarshalJSON() ([]byte, error) {
	out := make(map[string]interface{})
	out["declaration"] = t.Declaration
	out["did"] = t.Did
	out["displayName"] = t.DisplayName
	out["handle"] = t.Handle
	return json.Marshal(out)
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
