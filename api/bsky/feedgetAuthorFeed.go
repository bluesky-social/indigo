package schemagen

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/whyrusleeping/gosky/lex/util"
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
	Author        *ActorRef_WithInfo                `json:"author" cborgen:"author"`
	Cid           string                            `json:"cid" cborgen:"cid"`
	DownvoteCount int64                             `json:"downvoteCount" cborgen:"downvoteCount"`
	Embed         *FeedGetAuthorFeed_FeedItem_Embed `json:"embed" cborgen:"embed"`
	IndexedAt     string                            `json:"indexedAt" cborgen:"indexedAt"`
	MyState       *FeedGetAuthorFeed_MyState        `json:"myState" cborgen:"myState"`
	Record        any                               `json:"record" cborgen:"record"`
	ReplyCount    int64                             `json:"replyCount" cborgen:"replyCount"`
	RepostCount   int64                             `json:"repostCount" cborgen:"repostCount"`
	RepostedBy    *ActorRef_WithInfo                `json:"repostedBy" cborgen:"repostedBy"`
	TrendedBy     *ActorRef_WithInfo                `json:"trendedBy" cborgen:"trendedBy"`
	UpvoteCount   int64                             `json:"upvoteCount" cborgen:"upvoteCount"`
	Uri           string                            `json:"uri" cborgen:"uri"`
}

type FeedGetAuthorFeed_FeedItem_Embed struct {
	EmbedImages_Presented   *EmbedImages_Presented
	EmbedExternal_Presented *EmbedExternal_Presented
}

func (t *FeedGetAuthorFeed_FeedItem_Embed) MarshalJSON() ([]byte, error) {
	if t.EmbedImages_Presented != nil {
		return json.Marshal(t.EmbedImages_Presented)
	}
	if t.EmbedExternal_Presented != nil {
		return json.Marshal(t.EmbedExternal_Presented)
	}
	return nil, fmt.Errorf("cannot marshal empty enum")
}
func (t *FeedGetAuthorFeed_FeedItem_Embed) UnmarshalJSON(b []byte) error {
	typ, err := util.TypeExtract(b)
	if err != nil {
		return err
	}

	switch typ {
	case "app.bsky.embed.images#presented":
		t.EmbedImages_Presented = new(EmbedImages_Presented)
		return json.Unmarshal(b, t.EmbedImages_Presented)
	case "app.bsky.embed.external#presented":
		t.EmbedExternal_Presented = new(EmbedExternal_Presented)
		return json.Unmarshal(b, t.EmbedExternal_Presented)

	default:
		return nil
	}
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
