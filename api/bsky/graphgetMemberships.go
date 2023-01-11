package schemagen

import (
	"context"

	"github.com/whyrusleeping/gosky/xrpc"
)

// schema: app.bsky.graph.getMemberships

func init() {
}

type GraphGetMemberships_Membership struct {
	LexiconTypeID string         `json:"$type,omitempty"`
	CreatedAt     *string        `json:"createdAt,omitempty" cborgen:"createdAt"`
	Declaration   *SystemDeclRef `json:"declaration" cborgen:"declaration"`
	Did           string         `json:"did" cborgen:"did"`
	DisplayName   *string        `json:"displayName,omitempty" cborgen:"displayName"`
	Handle        string         `json:"handle" cborgen:"handle"`
	IndexedAt     string         `json:"indexedAt" cborgen:"indexedAt"`
}

type GraphGetMemberships_Output struct {
	LexiconTypeID string                            `json:"$type,omitempty"`
	Cursor        *string                           `json:"cursor,omitempty" cborgen:"cursor"`
	Memberships   []*GraphGetMemberships_Membership `json:"memberships" cborgen:"memberships"`
	Subject       *ActorRef_WithInfo                `json:"subject" cborgen:"subject"`
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
