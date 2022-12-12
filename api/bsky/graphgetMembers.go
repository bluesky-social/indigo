package schemagen

import (
	"context"
	"encoding/json"

	"github.com/whyrusleeping/gosky/xrpc"
)

// schema: app.bsky.graph.getMembers

type GraphGetMembers_Output struct {
	Cursor  string                    `json:"cursor" cborgen:"cursor"`
	Members []*GraphGetMembers_Member `json:"members" cborgen:"members"`
	Subject *ActorRef_WithInfo        `json:"subject" cborgen:"subject"`
}

func (t *GraphGetMembers_Output) MarshalJSON() ([]byte, error) {
	out := make(map[string]interface{})
	out["cursor"] = t.Cursor
	out["members"] = t.Members
	out["subject"] = t.Subject
	return json.Marshal(out)
}

type GraphGetMembers_Member struct {
	DisplayName string         `json:"displayName" cborgen:"displayName"`
	CreatedAt   string         `json:"createdAt" cborgen:"createdAt"`
	IndexedAt   string         `json:"indexedAt" cborgen:"indexedAt"`
	Did         string         `json:"did" cborgen:"did"`
	Declaration *SystemDeclRef `json:"declaration" cborgen:"declaration"`
	Handle      string         `json:"handle" cborgen:"handle"`
}

func (t *GraphGetMembers_Member) MarshalJSON() ([]byte, error) {
	out := make(map[string]interface{})
	out["createdAt"] = t.CreatedAt
	out["declaration"] = t.Declaration
	out["did"] = t.Did
	out["displayName"] = t.DisplayName
	out["handle"] = t.Handle
	out["indexedAt"] = t.IndexedAt
	return json.Marshal(out)
}

func GraphGetMembers(ctx context.Context, c *xrpc.Client, actor string, before string, limit int64) (*GraphGetMembers_Output, error) {
	var out GraphGetMembers_Output

	params := map[string]interface{}{
		"actor":  actor,
		"before": before,
		"limit":  limit,
	}
	if err := c.Do(ctx, xrpc.Query, "", "app.bsky.graph.getMembers", params, nil, &out); err != nil {
		return nil, err
	}

	return &out, nil
}
