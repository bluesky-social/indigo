package schemagen

import (
	"context"

	"github.com/whyrusleeping/gosky/xrpc"
)

// schema: app.bsky.feed.getVotes

func init() {
}

type FeedGetVotes_Output struct {
	LexiconTypeID string               `json:"$type,omitempty"`
	Cid           *string              `json:"cid,omitempty" cborgen:"cid"`
	Cursor        *string              `json:"cursor,omitempty" cborgen:"cursor"`
	Uri           string               `json:"uri" cborgen:"uri"`
	Votes         []*FeedGetVotes_Vote `json:"votes" cborgen:"votes"`
}

type FeedGetVotes_Vote struct {
	LexiconTypeID string             `json:"$type,omitempty"`
	Actor         *ActorRef_WithInfo `json:"actor" cborgen:"actor"`
	CreatedAt     string             `json:"createdAt" cborgen:"createdAt"`
	Direction     string             `json:"direction" cborgen:"direction"`
	IndexedAt     string             `json:"indexedAt" cborgen:"indexedAt"`
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
