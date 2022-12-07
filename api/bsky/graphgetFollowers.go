package schemagen

import (
	"context"
	"encoding/json"

	"github.com/whyrusleeping/gosky/xrpc"
)

// schema: app.bsky.graph.getFollowers

type GraphGetFollowers_Output struct {
	Subject   *GraphGetFollowers_Subject    `json:"subject"`
	Cursor    string                        `json:"cursor"`
	Followers []*GraphGetFollowers_Follower `json:"followers"`
}

func (t *GraphGetFollowers_Output) MarshalJSON() ([]byte, error) {
	out := make(map[string]interface{})
	out["cursor"] = t.Cursor
	out["followers"] = t.Followers
	out["subject"] = t.Subject
	return json.Marshal(out)
}

type GraphGetFollowers_Subject struct {
	DisplayName string         `json:"displayName"`
	Did         string         `json:"did"`
	Declaration *SystemDeclRef `json:"declaration"`
	Handle      string         `json:"handle"`
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
	IndexedAt   string         `json:"indexedAt"`
	Did         string         `json:"did"`
	Declaration *SystemDeclRef `json:"declaration"`
	Handle      string         `json:"handle"`
	DisplayName string         `json:"displayName"`
	CreatedAt   string         `json:"createdAt"`
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
