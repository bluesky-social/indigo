package bsky

import (
	"context"

	"github.com/bluesky-social/indigo/xrpc"
)

// schema: app.bsky.feed.getRepostedBy

func init() {
}

type FeedGetRepostedBy_Output struct {
	Cid        *string                  `json:"cid,omitempty" cborgen:"cid,omitempty"`
	Cursor     *string                  `json:"cursor,omitempty" cborgen:"cursor,omitempty"`
	RepostedBy []*ActorDefs_ProfileView `json:"repostedBy" cborgen:"repostedBy"`
	Uri        string                   `json:"uri" cborgen:"uri"`
}

func FeedGetRepostedBy(ctx context.Context, c *xrpc.Client, cid string, cursor string, limit int64, uri string) (*FeedGetRepostedBy_Output, error) {
	var out FeedGetRepostedBy_Output

	params := map[string]interface{}{
		"cid":    cid,
		"cursor": cursor,
		"limit":  limit,
		"uri":    uri,
	}
	if err := c.Do(ctx, xrpc.Query, "", "app.bsky.feed.getRepostedBy", params, nil, &out); err != nil {
		return nil, err
	}

	return &out, nil
}
