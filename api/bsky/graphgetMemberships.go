package schemagen

import (
	"context"
	"encoding/json"

	"github.com/whyrusleeping/gosky/xrpc"
)

// schema: app.bsky.graph.getMemberships

type GraphGetMemberships_Membership struct {
	Did         string         `json:"did" cborgen:"did"`
	Declaration *SystemDeclRef `json:"declaration" cborgen:"declaration"`
	Handle      string         `json:"handle" cborgen:"handle"`
	DisplayName string         `json:"displayName" cborgen:"displayName"`
	CreatedAt   string         `json:"createdAt" cborgen:"createdAt"`
	IndexedAt   string         `json:"indexedAt" cborgen:"indexedAt"`
}

func (t *GraphGetMemberships_Membership) MarshalJSON() ([]byte, error) {
	out := make(map[string]interface{})
	out["createdAt"] = t.CreatedAt
	out["declaration"] = t.Declaration
	out["did"] = t.Did
	out["displayName"] = t.DisplayName
	out["handle"] = t.Handle
	out["indexedAt"] = t.IndexedAt
	return json.Marshal(out)
}

type GraphGetMemberships_Output struct {
	Subject     *ActorRef_WithInfo                `json:"subject" cborgen:"subject"`
	Cursor      string                            `json:"cursor" cborgen:"cursor"`
	Memberships []*GraphGetMemberships_Membership `json:"memberships" cborgen:"memberships"`
}

func (t *GraphGetMemberships_Output) MarshalJSON() ([]byte, error) {
	out := make(map[string]interface{})
	out["cursor"] = t.Cursor
	out["memberships"] = t.Memberships
	out["subject"] = t.Subject
	return json.Marshal(out)
}

func GraphGetMemberships(ctx context.Context, c *xrpc.Client, actor string, before string, limit int64) (*GraphGetMemberships_Output, error) {
	var out GraphGetMemberships_Output

	params := map[string]interface{}{
		"actor":  actor,
		"before": before,
		"limit":  limit,
	}
	if err := c.Do(ctx, xrpc.Query, "", "app.bsky.graph.getMemberships", params, nil, &out); err != nil {
		return nil, err
	}

	return &out, nil
}
