package api

import (
	"context"
	"encoding/json"

	"github.com/whyrusleeping/gosky/xrpc"
)

type BskyApp struct {
	C *xrpc.Client
}

type PostEntity struct {
	Index []int  `json:"index"`
	Type  string `json:"type"`
	Value string `json:"value"`
}

type replyRef struct {
	Root   PostRef `json:"root"`
	Parent PostRef `json:"parent"`
}

type PostRecord struct {
	Text      string      `json:"text"`
	Entities  *PostEntity `json:"entities,omitempty"`
	Reply     *replyRef   `json:"reply,omitempty"`
	CreatedAt string      `json:"createdAt"`
}

func (pr PostRecord) Type() string {
	return "app.bsky.feed.post"
}

type JsonLD interface {
	Type() string
}

type RecordWrapper struct {
	Sub JsonLD
}

func (rw *RecordWrapper) MarshalJSON() ([]byte, error) {
	b, err := json.Marshal(rw.Sub)
	if err != nil {
		return nil, err
	}

	inject := "\"$type\":\"" + rw.Sub.Type() + "\","

	n := append([]byte("{"), []byte(inject)...)
	n = append(n, b[1:]...)

	return n, nil
}

type PostRef struct {
	Uri string `json:"uri"`
	Cid string `json:"cid"`
}

type GetTimelineResp struct {
	Cursor string     `json:"cursor"`
	Feed   []FeedItem `json:"feed"`
}

type FeedItem struct {
	Uri        string      `json:"uri"`
	Cid        string      `json:"cid"`
	Author     *User       `json:"author"`
	RepostedBy *User       `json:"repostedBy"`
	Record     interface{} `json:"record"`
}

type User struct {
	Did         string `json:"did"`
	Handle      string `json:"handle"`
	DisplayName string `json:"displayName"`
}

func (b *BskyApp) FeedGetTimeline(ctx context.Context, algo string, limit int, before *string) (*GetTimelineResp, error) {
	params := map[string]interface{}{
		"algorithm": algo,
		"limit":     limit,
	}

	if before != nil {
		params["before"] = *before
	}

	var out GetTimelineResp
	if err := b.C.Do(ctx, xrpc.Query, "app.bsky.feed.getTimeline", params, nil, &out); err != nil {
		return nil, err
	}

	return &out, nil
}

func (b *BskyApp) FeedGetAuthorFeed(ctx context.Context, author string, limit int, before *string) (*GetTimelineResp, error) {
	params := map[string]interface{}{
		"author": author,
		"limit":  limit,
	}

	if before != nil {
		params["before"] = *before
	}

	var out GetTimelineResp
	if err := b.C.Do(ctx, xrpc.Query, "app.bsky.feed.getAuthorFeed", params, nil, &out); err != nil {
		return nil, err
	}

	return &out, nil
}
