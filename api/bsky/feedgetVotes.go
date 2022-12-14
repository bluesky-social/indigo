package schemagen

import (
	"context"

	"github.com/whyrusleeping/gosky/xrpc"
)

// schema: app.bsky.feed.getVotes

func init() {
}

type FeedGetVotes_Output struct {
	Uri    string               `json:"uri" cborgen:"uri"`
	Cid    *string              `json:"cid" cborgen:"cid"`
	Cursor *string              `json:"cursor" cborgen:"cursor"`
	Votes  []*FeedGetVotes_Vote `json:"votes" cborgen:"votes"`
}

type FeedGetVotes_Vote struct {
	Direction string             `json:"direction" cborgen:"direction"`
	IndexedAt string             `json:"indexedAt" cborgen:"indexedAt"`
	CreatedAt string             `json:"createdAt" cborgen:"createdAt"`
	Actor     *ActorRef_WithInfo `json:"actor" cborgen:"actor"`
}

func FeedGetVotes(ctx context.Context, c *xrpc.Client, before string, cid string, direction string, limit int64, uri string) (*FeedGetVotes_Output, error) {
	var out FeedGetVotes_Output

	params := map[string]interface{}{
		"before":    before,
		"cid":       cid,
		"direction": direction,
		"limit":     limit,
		"uri":       uri,
	}
	if err := c.Do(ctx, xrpc.Query, "", "app.bsky.feed.getVotes", params, nil, &out); err != nil {
		return nil, err
	}

	return &out, nil
}
