package schemagen

import (
	"context"
	"encoding/json"

	"github.com/whyrusleeping/gosky/xrpc"
)

// schema: app.bsky.graph.getFollows

type GraphGetFollows_Output struct {
	Follows []*GraphGetFollows_Follow `json:"follows"`
	Subject *ActorRef_WithInfo        `json:"subject"`
	Cursor  string                    `json:"cursor"`
}

func (t *GraphGetFollows_Output) MarshalJSON() ([]byte, error) {
	out := make(map[string]interface{})
	out["cursor"] = t.Cursor
	out["follows"] = t.Follows
	out["subject"] = t.Subject
	return json.Marshal(out)
}

type GraphGetFollows_Follow struct {
	Declaration *SystemDeclRef `json:"declaration"`
	Handle      string         `json:"handle"`
	DisplayName string         `json:"displayName"`
	CreatedAt   string         `json:"createdAt"`
	IndexedAt   string         `json:"indexedAt"`
	Did         string         `json:"did"`
}

func (t *GraphGetFollows_Follow) MarshalJSON() ([]byte, error) {
	out := make(map[string]interface{})
	out["createdAt"] = t.CreatedAt
	out["declaration"] = t.Declaration
	out["did"] = t.Did
	out["displayName"] = t.DisplayName
	out["handle"] = t.Handle
	out["indexedAt"] = t.IndexedAt
	return json.Marshal(out)
}

func GraphGetFollows(ctx context.Context, c *xrpc.Client, before string, limit int64, user string) (*GraphGetFollows_Output, error) {
	var out GraphGetFollows_Output

	params := map[string]interface{}{
		"before": before,
		"limit":  limit,
		"user":   user,
	}
	if err := c.Do(ctx, xrpc.Query, "", "app.bsky.graph.getFollows", params, nil, &out); err != nil {
		return nil, err
	}

	return &out, nil
}
