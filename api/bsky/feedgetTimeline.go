package schemagen

import (
	"context"
	"encoding/json"

	"github.com/whyrusleeping/gosky/xrpc"
)

// schema: app.bsky.feed.getTimeline

type FeedGetTimeline_Output struct {
	Cursor string                      `json:"cursor" cborgen:"cursor"`
	Feed   []*FeedGetTimeline_FeedItem `json:"feed" cborgen:"feed"`
}

func (t *FeedGetTimeline_Output) MarshalJSON() ([]byte, error) {
	out := make(map[string]interface{})
	out["cursor"] = t.Cursor
	out["feed"] = t.Feed
	return json.Marshal(out)
}

type FeedGetTimeline_FeedItem struct {
	Cid           string                   `json:"cid" cborgen:"cid"`
	RepostedBy    *ActorRef_WithInfo       `json:"repostedBy" cborgen:"repostedBy"`
	Embed         *FeedEmbed               `json:"embed" cborgen:"embed"`
	RepostCount   int64                    `json:"repostCount" cborgen:"repostCount"`
	DownvoteCount int64                    `json:"downvoteCount" cborgen:"downvoteCount"`
	MyState       *FeedGetTimeline_MyState `json:"myState" cborgen:"myState"`
	Uri           string                   `json:"uri" cborgen:"uri"`
	Author        *ActorRef_WithInfo       `json:"author" cborgen:"author"`
	TrendedBy     *ActorRef_WithInfo       `json:"trendedBy" cborgen:"trendedBy"`
	Record        any                      `json:"record" cborgen:"record"`
	ReplyCount    int64                    `json:"replyCount" cborgen:"replyCount"`
	UpvoteCount   int64                    `json:"upvoteCount" cborgen:"upvoteCount"`
	IndexedAt     string                   `json:"indexedAt" cborgen:"indexedAt"`
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
	Repost   string `json:"repost" cborgen:"repost"`
	Upvote   string `json:"upvote" cborgen:"upvote"`
	Downvote string `json:"downvote" cborgen:"downvote"`
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
