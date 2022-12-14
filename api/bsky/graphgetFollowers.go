package schemagen

import (
	"context"

	"github.com/whyrusleeping/gosky/xrpc"
)

// schema: app.bsky.graph.getFollowers

func init() {
}

type GraphGetFollowers_Output struct {
	Subject   *GraphGetFollowers_Subject    `json:"subject" cborgen:"subject"`
	Cursor    *string                       `json:"cursor" cborgen:"cursor"`
	Followers []*GraphGetFollowers_Follower `json:"followers" cborgen:"followers"`
}

type GraphGetFollowers_Subject struct {
	Did         string         `json:"did" cborgen:"did"`
	Declaration *SystemDeclRef `json:"declaration" cborgen:"declaration"`
	Handle      string         `json:"handle" cborgen:"handle"`
	DisplayName *string        `json:"displayName" cborgen:"displayName"`
}

type GraphGetFollowers_Follower struct {
	CreatedAt   *string        `json:"createdAt" cborgen:"createdAt"`
	IndexedAt   string         `json:"indexedAt" cborgen:"indexedAt"`
	Did         string         `json:"did" cborgen:"did"`
	Declaration *SystemDeclRef `json:"declaration" cborgen:"declaration"`
	Handle      string         `json:"handle" cborgen:"handle"`
	DisplayName *string        `json:"displayName" cborgen:"displayName"`
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
