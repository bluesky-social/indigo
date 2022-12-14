package schemagen

import (
	"context"

	"github.com/whyrusleeping/gosky/xrpc"
)

// schema: app.bsky.graph.getFollows

func init() {
}

type GraphGetFollows_Output struct {
	Cursor  *string                   `json:"cursor" cborgen:"cursor"`
	Follows []*GraphGetFollows_Follow `json:"follows" cborgen:"follows"`
	Subject *ActorRef_WithInfo        `json:"subject" cborgen:"subject"`
}

type GraphGetFollows_Follow struct {
	Did         string         `json:"did" cborgen:"did"`
	Declaration *SystemDeclRef `json:"declaration" cborgen:"declaration"`
	Handle      string         `json:"handle" cborgen:"handle"`
	DisplayName *string        `json:"displayName" cborgen:"displayName"`
	CreatedAt   *string        `json:"createdAt" cborgen:"createdAt"`
	IndexedAt   string         `json:"indexedAt" cborgen:"indexedAt"`
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
