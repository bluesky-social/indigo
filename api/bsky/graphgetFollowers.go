package schemagen

import (
	"context"
	"encoding/json"

	"github.com/whyrusleeping/gosky/xrpc"
)

// schema: app.bsky.graph.getFollowers

type GraphGetFollowers_Subject struct {
	Did         string         `json:"did" cborgen:"did"`
	Declaration *SystemDeclRef `json:"declaration" cborgen:"declaration"`
	Handle      string         `json:"handle" cborgen:"handle"`
	DisplayName string         `json:"displayName" cborgen:"displayName"`
}

func (t *GraphGetFollowers_Subject) MarshalJSON() ([]byte, error) {
	out := make(map[string]interface{})
	out["declaration"] = t.Declaration
	out["did"] = t.Did
	out["displayName"] = t.DisplayName
	out["handle"] = t.Handle
	return json.Marshal(out)
}

type GraphGetFollowers_Follower struct {
	Did         string         `json:"did" cborgen:"did"`
	Declaration *SystemDeclRef `json:"declaration" cborgen:"declaration"`
	Handle      string         `json:"handle" cborgen:"handle"`
	DisplayName string         `json:"displayName" cborgen:"displayName"`
	CreatedAt   string         `json:"createdAt" cborgen:"createdAt"`
	IndexedAt   string         `json:"indexedAt" cborgen:"indexedAt"`
}

func (t *GraphGetFollowers_Follower) MarshalJSON() ([]byte, error) {
	out := make(map[string]interface{})
	out["createdAt"] = t.CreatedAt
	out["declaration"] = t.Declaration
	out["did"] = t.Did
	out["displayName"] = t.DisplayName
	out["handle"] = t.Handle
	out["indexedAt"] = t.IndexedAt
	return json.Marshal(out)
}

type GraphGetFollowers_Output struct {
	Subject   *GraphGetFollowers_Subject    `json:"subject" cborgen:"subject"`
	Cursor    string                        `json:"cursor" cborgen:"cursor"`
	Followers []*GraphGetFollowers_Follower `json:"followers" cborgen:"followers"`
}

func (t *GraphGetFollowers_Output) MarshalJSON() ([]byte, error) {
	out := make(map[string]interface{})
	out["cursor"] = t.Cursor
	out["followers"] = t.Followers
	out["subject"] = t.Subject
	return json.Marshal(out)
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
