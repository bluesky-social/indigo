package schemagen

import (
	"context"
	"encoding/json"

	"github.com/whyrusleeping/gosky/xrpc"
)

// schema: app.bsky.feed.getRepostedBy

type FeedGetRepostedBy_Output struct {
	Uri        string                          `json:"uri"`
	Cid        string                          `json:"cid"`
	Cursor     string                          `json:"cursor"`
	RepostedBy []*FeedGetRepostedBy_RepostedBy `json:"repostedBy"`
}

func (t *FeedGetRepostedBy_Output) MarshalJSON() ([]byte, error) {
	out := make(map[string]interface{})
	out["cid"] = t.Cid
	out["cursor"] = t.Cursor
	out["repostedBy"] = t.RepostedBy
	out["uri"] = t.Uri
	return json.Marshal(out)
}

type FeedGetRepostedBy_RepostedBy struct {
	Did         string         `json:"did"`
	Declaration *SystemDeclRef `json:"declaration"`
	Handle      string         `json:"handle"`
	DisplayName string         `json:"displayName"`
	CreatedAt   string         `json:"createdAt"`
	IndexedAt   string         `json:"indexedAt"`
}

func (t *FeedGetRepostedBy_RepostedBy) MarshalJSON() ([]byte, error) {
	out := make(map[string]interface{})
	out["createdAt"] = t.CreatedAt
	out["declaration"] = t.Declaration
	out["did"] = t.Did
	out["displayName"] = t.DisplayName
	out["handle"] = t.Handle
	out["indexedAt"] = t.IndexedAt
	return json.Marshal(out)
}

func FeedGetRepostedBy(ctx context.Context, c *xrpc.Client, before string, cid string, limit int64, uri string) (*FeedGetRepostedBy_Output, error) {
	var out FeedGetRepostedBy_Output

	params := map[string]interface{}{
		"before": before,
		"cid":    cid,
		"limit":  limit,
		"uri":    uri,
	}
	if err := c.Do(ctx, xrpc.Query, "", "app.bsky.feed.getRepostedBy", params, nil, &out); err != nil {
		return nil, err
	}

	return &out, nil
}
