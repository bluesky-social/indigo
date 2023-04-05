package bsky

import (
	"context"

	"github.com/bluesky-social/indigo/xrpc"
)

// schema: app.bsky.feed.getLikes

func init() {
}

type FeedGetLikes_Like struct {
	Actor     *ActorDefs_ProfileView `json:"actor" cborgen:"actor"`
	CreatedAt string                 `json:"createdAt" cborgen:"createdAt"`
	IndexedAt string                 `json:"indexedAt" cborgen:"indexedAt"`
}

type FeedGetLikes_Output struct {
	Cid    *string              `json:"cid,omitempty" cborgen:"cid,omitempty"`
	Cursor *string              `json:"cursor,omitempty" cborgen:"cursor,omitempty"`
	Likes  []*FeedGetLikes_Like `json:"likes" cborgen:"likes"`
	Uri    string               `json:"uri" cborgen:"uri"`
}

func FeedGetLikes(ctx context.Context, c *xrpc.Client, cid string, cursor string, limit int64, uri string) (*FeedGetLikes_Output, error) {
	var out FeedGetLikes_Output

	params := map[string]interface{}{
		"cid":    cid,
		"cursor": cursor,
		"limit":  limit,
		"uri":    uri,
	}
	if err := c.Do(ctx, xrpc.Query, "", "app.bsky.feed.getLikes", params, nil, &out); err != nil {
		return nil, err
	}

	return &out, nil
}
