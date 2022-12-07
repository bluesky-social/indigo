package schemagen

import (
	"context"
	"encoding/json"

	"github.com/whyrusleeping/gosky/xrpc"
)

// schema: app.bsky.feed.getAuthorFeed

type FeedGetAuthorFeed_Output struct {
	Cursor string                        `json:"cursor"`
	Feed   []*FeedGetAuthorFeed_FeedItem `json:"feed"`
}

func (t *FeedGetAuthorFeed_Output) MarshalJSON() ([]byte, error) {
	out := make(map[string]interface{})
	out["cursor"] = t.Cursor
	out["feed"] = t.Feed
	return json.Marshal(out)
}

type FeedGetAuthorFeed_FeedItem struct {
	IndexedAt     string                     `json:"indexedAt"`
	Cid           string                     `json:"cid"`
	TrendedBy     *ActorRef_WithInfo         `json:"trendedBy"`
	Record        any                        `json:"record"`
	ReplyCount    int64                      `json:"replyCount"`
	RepostCount   int64                      `json:"repostCount"`
	UpvoteCount   int64                      `json:"upvoteCount"`
	DownvoteCount int64                      `json:"downvoteCount"`
	MyState       *FeedGetAuthorFeed_MyState `json:"myState"`
	Uri           string                     `json:"uri"`
	Author        *ActorRef_WithInfo         `json:"author"`
	RepostedBy    *ActorRef_WithInfo         `json:"repostedBy"`
	Embed         *FeedEmbed                 `json:"embed"`
}

func (t *FeedGetAuthorFeed_FeedItem) MarshalJSON() ([]byte, error) {
	out := make(map[string]interface{})
	out["author"] = t.Author
	out["cid"] = t.Cid
	out["downvoteCount"] = t.DownvoteCount
	out["embed"] = t.Embed
	out["indexedAt"] = t.IndexedAt
	out["myState"] = t.MyState
	out["record"] = t.Record
	out["replyCount"] = t.ReplyCount
	out["repostCount"] = t.RepostCount
	out["repostedBy"] = t.RepostedBy
	out["trendedBy"] = t.TrendedBy
	out["upvoteCount"] = t.UpvoteCount
	out["uri"] = t.Uri
	return json.Marshal(out)
}

type FeedGetAuthorFeed_MyState struct {
	Downvote string `json:"downvote"`
	Repost   string `json:"repost"`
	Upvote   string `json:"upvote"`
}

func (t *FeedGetAuthorFeed_MyState) MarshalJSON() ([]byte, error) {
	out := make(map[string]interface{})
	out["downvote"] = t.Downvote
	out["repost"] = t.Repost
	out["upvote"] = t.Upvote
	return json.Marshal(out)
}

func FeedGetAuthorFeed(ctx context.Context, c *xrpc.Client, author string, before string, limit int64) (*FeedGetAuthorFeed_Output, error) {
	var out FeedGetAuthorFeed_Output

	params := map[string]interface{}{
		"author": author,
		"before": before,
		"limit":  limit,
	}
	if err := c.Do(ctx, xrpc.Query, "", "app.bsky.feed.getAuthorFeed", params, nil, &out); err != nil {
		return nil, err
	}

	return &out, nil
}
