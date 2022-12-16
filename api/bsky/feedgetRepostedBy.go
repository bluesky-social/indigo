package schemagen

import (
	"context"

	"github.com/whyrusleeping/gosky/xrpc"
)

// schema: app.bsky.feed.getRepostedBy

func init() {
}

type FeedGetRepostedBy_Output struct {
	Cursor     *string                         `json:"cursor" cborgen:"cursor"`
	RepostedBy []*FeedGetRepostedBy_RepostedBy `json:"repostedBy" cborgen:"repostedBy"`
	Uri        string                          `json:"uri" cborgen:"uri"`
	Cid        *string                         `json:"cid" cborgen:"cid"`
}

type FeedGetRepostedBy_RepostedBy struct {
	Did         string         `json:"did" cborgen:"did"`
	Declaration *SystemDeclRef `json:"declaration" cborgen:"declaration"`
	Handle      string         `json:"handle" cborgen:"handle"`
	DisplayName *string        `json:"displayName" cborgen:"displayName"`
	CreatedAt   *string        `json:"createdAt" cborgen:"createdAt"`
	IndexedAt   string         `json:"indexedAt" cborgen:"indexedAt"`
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
