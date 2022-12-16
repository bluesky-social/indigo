package schemagen

import (
	"context"

	"github.com/whyrusleeping/gosky/xrpc"
)

// schema: app.bsky.feed.getAuthorFeed

func init() {
}

type FeedGetAuthorFeed_Output struct {
	Cursor *string                       `json:"cursor" cborgen:"cursor"`
	Feed   []*FeedGetAuthorFeed_FeedItem `json:"feed" cborgen:"feed"`
}

type FeedGetAuthorFeed_FeedItem struct {
	ReplyCount    int64                      `json:"replyCount" cborgen:"replyCount"`
	UpvoteCount   int64                      `json:"upvoteCount" cborgen:"upvoteCount"`
	MyState       *FeedGetAuthorFeed_MyState `json:"myState" cborgen:"myState"`
	Uri           string                     `json:"uri" cborgen:"uri"`
	Author        *ActorRef_WithInfo         `json:"author" cborgen:"author"`
	RepostedBy    *ActorRef_WithInfo         `json:"repostedBy" cborgen:"repostedBy"`
	Record        any                        `json:"record" cborgen:"record"`
	Embed         *FeedEmbed                 `json:"embed" cborgen:"embed"`
	Cid           string                     `json:"cid" cborgen:"cid"`
	TrendedBy     *ActorRef_WithInfo         `json:"trendedBy" cborgen:"trendedBy"`
	RepostCount   int64                      `json:"repostCount" cborgen:"repostCount"`
	DownvoteCount int64                      `json:"downvoteCount" cborgen:"downvoteCount"`
	IndexedAt     string                     `json:"indexedAt" cborgen:"indexedAt"`
}

type FeedGetAuthorFeed_MyState struct {
	Downvote *string `json:"downvote" cborgen:"downvote"`
	Repost   *string `json:"repost" cborgen:"repost"`
	Upvote   *string `json:"upvote" cborgen:"upvote"`
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
