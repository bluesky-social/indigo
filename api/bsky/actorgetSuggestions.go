package schemagen

import (
	"context"
	"encoding/json"

	"github.com/whyrusleeping/gosky/xrpc"
)

// schema: app.bsky.actor.getSuggestions

type ActorGetSuggestions_Output struct {
	Cursor string                       `json:"cursor"`
	Actors []*ActorGetSuggestions_Actor `json:"actors"`
}

func (t *ActorGetSuggestions_Output) MarshalJSON() ([]byte, error) {
	out := make(map[string]interface{})
	out["actors"] = t.Actors
	out["cursor"] = t.Cursor
	return json.Marshal(out)
}

type ActorGetSuggestions_Actor struct {
	MyState     *ActorGetSuggestions_MyState `json:"myState"`
	Did         string                       `json:"did"`
	Declaration *SystemDeclRef               `json:"declaration"`
	Handle      string                       `json:"handle"`
	DisplayName string                       `json:"displayName"`
	Description string                       `json:"description"`
	IndexedAt   string                       `json:"indexedAt"`
}

func (t *ActorGetSuggestions_Actor) MarshalJSON() ([]byte, error) {
	out := make(map[string]interface{})
	out["declaration"] = t.Declaration
	out["description"] = t.Description
	out["did"] = t.Did
	out["displayName"] = t.DisplayName
	out["handle"] = t.Handle
	out["indexedAt"] = t.IndexedAt
	out["myState"] = t.MyState
	return json.Marshal(out)
}

type ActorGetSuggestions_MyState struct {
	Follow string `json:"follow"`
}

func (t *ActorGetSuggestions_MyState) MarshalJSON() ([]byte, error) {
	out := make(map[string]interface{})
	out["follow"] = t.Follow
	return json.Marshal(out)
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
