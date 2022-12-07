package schemagen

import (
	"context"
	"encoding/json"

	"github.com/whyrusleeping/gosky/xrpc"
)

// schema: app.bsky.feed.getTimeline

type FeedGetTimeline_Output struct {
	Cursor string                      `json:"cursor"`
	Feed   []*FeedGetTimeline_FeedItem `json:"feed"`
}

func (t *FeedGetTimeline_Output) MarshalJSON() ([]byte, error) {
	out := make(map[string]interface{})
	out["cursor"] = t.Cursor
	out["feed"] = t.Feed
	return json.Marshal(out)
}

type FeedGetTimeline_FeedItem struct {
	Embed         *FeedEmbed               `json:"embed"`
	ReplyCount    int64                    `json:"replyCount"`
	DownvoteCount int64                    `json:"downvoteCount"`
	Author        *ActorRef_WithInfo       `json:"author"`
	TrendedBy     *ActorRef_WithInfo       `json:"trendedBy"`
	RepostedBy    *ActorRef_WithInfo       `json:"repostedBy"`
	RepostCount   int64                    `json:"repostCount"`
	UpvoteCount   int64                    `json:"upvoteCount"`
	IndexedAt     string                   `json:"indexedAt"`
	MyState       *FeedGetTimeline_MyState `json:"myState"`
	Uri           string                   `json:"uri"`
	Cid           string                   `json:"cid"`
	Record        any                      `json:"record"`
}

func (t *FeedGetTimeline_FeedItem) MarshalJSON() ([]byte, error) {
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

type FeedGetTimeline_MyState struct {
	Repost   string `json:"repost"`
	Upvote   string `json:"upvote"`
	Downvote string `json:"downvote"`
}

func (t *FeedGetTimeline_MyState) MarshalJSON() ([]byte, error) {
	out := make(map[string]interface{})
	out["downvote"] = t.Downvote
	out["repost"] = t.Repost
	out["upvote"] = t.Upvote
	return json.Marshal(out)
}

func FeedGetTimeline(ctx context.Context, c *xrpc.Client, algorithm string, before string, limit int64) (*FeedGetTimeline_Output, error) {
	var out FeedGetTimeline_Output

	params := map[string]interface{}{
		"algorithm": algorithm,
		"before":    before,
		"limit":     limit,
	}
	if err := c.Do(ctx, xrpc.Query, "", "app.bsky.feed.getTimeline", params, nil, &out); err != nil {
		return nil, err
	}

	return &out, nil
}
