package schemagen

import (
	"context"
	"encoding/json"

	"github.com/whyrusleeping/gosky/xrpc"
)

// schema: app.bsky.actor.search

type ActorSearch_Output struct {
	Cursor string              `json:"cursor"`
	Users  []*ActorSearch_User `json:"users"`
}

func (t *ActorSearch_Output) MarshalJSON() ([]byte, error) {
	out := make(map[string]interface{})
	out["cursor"] = t.Cursor
	out["users"] = t.Users
	return json.Marshal(out)
}

type ActorSearch_User struct {
	Did         string         `json:"did"`
	Declaration *SystemDeclRef `json:"declaration"`
	Handle      string         `json:"handle"`
	DisplayName string         `json:"displayName"`
	Description string         `json:"description"`
	IndexedAt   string         `json:"indexedAt"`
}

func (t *ActorSearch_User) MarshalJSON() ([]byte, error) {
	out := make(map[string]interface{})
	out["declaration"] = t.Declaration
	out["description"] = t.Description
	out["did"] = t.Did
	out["displayName"] = t.DisplayName
	out["handle"] = t.Handle
	out["indexedAt"] = t.IndexedAt
	return json.Marshal(out)
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
