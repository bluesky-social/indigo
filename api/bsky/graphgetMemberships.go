package schemagen

import (
	"context"

	"github.com/whyrusleeping/gosky/xrpc"
)

// schema: app.bsky.graph.getMemberships

func init() {
}

type GraphGetMemberships_Output struct {
	Subject     *ActorRef_WithInfo                `json:"subject" cborgen:"subject"`
	Cursor      *string                           `json:"cursor" cborgen:"cursor"`
	Memberships []*GraphGetMemberships_Membership `json:"memberships" cborgen:"memberships"`
}

type GraphGetMemberships_Membership struct {
	Declaration *SystemDeclRef `json:"declaration" cborgen:"declaration"`
	Handle      string         `json:"handle" cborgen:"handle"`
	DisplayName *string        `json:"displayName" cborgen:"displayName"`
	CreatedAt   *string        `json:"createdAt" cborgen:"createdAt"`
	IndexedAt   string         `json:"indexedAt" cborgen:"indexedAt"`
	Did         string         `json:"did" cborgen:"did"`
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
