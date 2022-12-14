package schemagen

import (
	"context"

	"github.com/whyrusleeping/gosky/xrpc"
)

// schema: app.bsky.feed.getAuthorFeed

func init() {
}

type FeedGetAuthorFeed_Output struct {
	Feed   []*FeedGetAuthorFeed_FeedItem `json:"feed" cborgen:"feed"`
	Cursor *string                       `json:"cursor" cborgen:"cursor"`
}

type FeedGetAuthorFeed_FeedItem struct {
	IndexedAt     string                     `json:"indexedAt" cborgen:"indexedAt"`
	RepostedBy    *ActorRef_WithInfo         `json:"repostedBy" cborgen:"repostedBy"`
	ReplyCount    int64                      `json:"replyCount" cborgen:"replyCount"`
	Author        *ActorRef_WithInfo         `json:"author" cborgen:"author"`
	TrendedBy     *ActorRef_WithInfo         `json:"trendedBy" cborgen:"trendedBy"`
	Record        any                        `json:"record" cborgen:"record"`
	Embed         *FeedEmbed                 `json:"embed" cborgen:"embed"`
	RepostCount   int64                      `json:"repostCount" cborgen:"repostCount"`
	UpvoteCount   int64                      `json:"upvoteCount" cborgen:"upvoteCount"`
	Uri           string                     `json:"uri" cborgen:"uri"`
	Cid           string                     `json:"cid" cborgen:"cid"`
	DownvoteCount int64                      `json:"downvoteCount" cborgen:"downvoteCount"`
	MyState       *FeedGetAuthorFeed_MyState `json:"myState" cborgen:"myState"`
}

type FeedGetAuthorFeed_MyState struct {
	Repost   string `json:"repost" cborgen:"repost"`
	Upvote   string `json:"upvote" cborgen:"upvote"`
	Downvote string `json:"downvote" cborgen:"downvote"`
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
