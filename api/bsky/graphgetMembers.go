package schemagen

import (
	"context"

	"github.com/whyrusleeping/gosky/xrpc"
)

// schema: app.bsky.graph.getMembers

func init() {
}

type GraphGetMembers_Member struct {
	LexiconTypeID string         `json:"$type,omitempty"`
	CreatedAt     *string        `json:"createdAt" cborgen:"createdAt"`
	Declaration   *SystemDeclRef `json:"declaration" cborgen:"declaration"`
	Did           string         `json:"did" cborgen:"did"`
	DisplayName   *string        `json:"displayName" cborgen:"displayName"`
	Handle        string         `json:"handle" cborgen:"handle"`
	IndexedAt     string         `json:"indexedAt" cborgen:"indexedAt"`
}

type GraphGetMembers_Output struct {
	LexiconTypeID string                    `json:"$type,omitempty"`
	Cursor        *string                   `json:"cursor" cborgen:"cursor"`
	Members       []*GraphGetMembers_Member `json:"members" cborgen:"members"`
	Subject       *ActorRef_WithInfo        `json:"subject" cborgen:"subject"`
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
