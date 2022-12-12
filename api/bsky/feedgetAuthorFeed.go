package schemagen

import (
	"context"
	"encoding/json"

	"github.com/whyrusleeping/gosky/xrpc"
)

// schema: app.bsky.feed.getAuthorFeed

type FeedGetAuthorFeed_Output struct {
	Cursor string                        `json:"cursor" cborgen:"cursor"`
	Feed   []*FeedGetAuthorFeed_FeedItem `json:"feed" cborgen:"feed"`
}

func (t *FeedGetAuthorFeed_Output) MarshalJSON() ([]byte, error) {
	out := make(map[string]interface{})
	out["cursor"] = t.Cursor
	out["feed"] = t.Feed
	return json.Marshal(out)
}

type FeedGetAuthorFeed_FeedItem struct {
	Uri           string                     `json:"uri" cborgen:"uri"`
	RepostedBy    *ActorRef_WithInfo         `json:"repostedBy" cborgen:"repostedBy"`
	Record        any                        `json:"record" cborgen:"record"`
	ReplyCount    int64                      `json:"replyCount" cborgen:"replyCount"`
	RepostCount   int64                      `json:"repostCount" cborgen:"repostCount"`
	UpvoteCount   int64                      `json:"upvoteCount" cborgen:"upvoteCount"`
	DownvoteCount int64                      `json:"downvoteCount" cborgen:"downvoteCount"`
	MyState       *FeedGetAuthorFeed_MyState `json:"myState" cborgen:"myState"`
	Cid           string                     `json:"cid" cborgen:"cid"`
	Author        *ActorRef_WithInfo         `json:"author" cborgen:"author"`
	TrendedBy     *ActorRef_WithInfo         `json:"trendedBy" cborgen:"trendedBy"`
	Embed         *FeedEmbed                 `json:"embed" cborgen:"embed"`
	IndexedAt     string                     `json:"indexedAt" cborgen:"indexedAt"`
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
	Upvote   string `json:"upvote" cborgen:"upvote"`
	Downvote string `json:"downvote" cborgen:"downvote"`
	Repost   string `json:"repost" cborgen:"repost"`
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
