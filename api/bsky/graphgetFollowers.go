package schemagen

import (
	"context"

	"github.com/whyrusleeping/gosky/xrpc"
)

// schema: app.bsky.graph.getFollowers

func init() {
}

type GraphGetFollowers_Follower struct {
	Avatar      *string        `json:"avatar" cborgen:"avatar"`
	CreatedAt   *string        `json:"createdAt" cborgen:"createdAt"`
	Declaration *SystemDeclRef `json:"declaration" cborgen:"declaration"`
	Did         string         `json:"did" cborgen:"did"`
	DisplayName *string        `json:"displayName" cborgen:"displayName"`
	Handle      string         `json:"handle" cborgen:"handle"`
	IndexedAt   string         `json:"indexedAt" cborgen:"indexedAt"`
}

type GraphGetFollowers_Output struct {
	Cursor    *string                       `json:"cursor" cborgen:"cursor"`
	Followers []*GraphGetFollowers_Follower `json:"followers" cborgen:"followers"`
	Subject   *ActorRef_WithInfo            `json:"subject" cborgen:"subject"`
}

func GraphGetFollowers(ctx context.Context, c *xrpc.Client, before string, limit int64, user string) (*GraphGetFollowers_Output, error) {
	var out GraphGetFollowers_Output

	params := map[string]interface{}{
		"before": before,
		"limit":  limit,
		"user":   user,
	}
	if err := c.Do(ctx, xrpc.Query, "", "app.bsky.graph.getFollowers", params, nil, &out); err != nil {
		return nil, err
	}

	return &out, nil
}
