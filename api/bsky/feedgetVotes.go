package schemagen

import (
	"context"
	"encoding/json"

	"github.com/whyrusleeping/gosky/xrpc"
)

// schema: app.bsky.feed.getVotes

type FeedGetVotes_Output struct {
	Uri    string               `json:"uri" cborgen:"uri"`
	Cid    string               `json:"cid" cborgen:"cid"`
	Cursor string               `json:"cursor" cborgen:"cursor"`
	Votes  []*FeedGetVotes_Vote `json:"votes" cborgen:"votes"`
}

func (t *FeedGetVotes_Output) MarshalJSON() ([]byte, error) {
	out := make(map[string]interface{})
	out["cid"] = t.Cid
	out["cursor"] = t.Cursor
	out["uri"] = t.Uri
	out["votes"] = t.Votes
	return json.Marshal(out)
}

type FeedGetVotes_Vote struct {
	Direction string             `json:"direction" cborgen:"direction"`
	IndexedAt string             `json:"indexedAt" cborgen:"indexedAt"`
	CreatedAt string             `json:"createdAt" cborgen:"createdAt"`
	Actor     *ActorRef_WithInfo `json:"actor" cborgen:"actor"`
}

func (t *FeedGetVotes_Vote) MarshalJSON() ([]byte, error) {
	out := make(map[string]interface{})
	out["actor"] = t.Actor
	out["createdAt"] = t.CreatedAt
	out["direction"] = t.Direction
	out["indexedAt"] = t.IndexedAt
	return json.Marshal(out)
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
