package schemagen

import (
	"context"

	"github.com/whyrusleeping/gosky/xrpc"
)

// schema: app.bsky.graph.getFollows

func init() {
}

type GraphGetFollows_Follow struct {
	LexiconTypeID string         `json:"$type,omitempty"`
	CreatedAt     *string        `json:"createdAt,omitempty" cborgen:"createdAt"`
	Declaration   *SystemDeclRef `json:"declaration" cborgen:"declaration"`
	Did           string         `json:"did" cborgen:"did"`
	DisplayName   *string        `json:"displayName,omitempty" cborgen:"displayName"`
	Handle        string         `json:"handle" cborgen:"handle"`
	IndexedAt     string         `json:"indexedAt" cborgen:"indexedAt"`
}

type GraphGetFollows_Output struct {
	LexiconTypeID string                    `json:"$type,omitempty"`
	Cursor        *string                   `json:"cursor,omitempty" cborgen:"cursor"`
	Follows       []*GraphGetFollows_Follow `json:"follows" cborgen:"follows"`
	Subject       *ActorRef_WithInfo        `json:"subject" cborgen:"subject"`
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
