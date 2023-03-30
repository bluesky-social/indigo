package bsky

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/bluesky-social/indigo/lex/util"
	"github.com/bluesky-social/indigo/xrpc"
)

// schema: app.bsky.feed.getPostThread

func init() {
}

type FeedGetPostThread_Output struct {
	LexiconTypeID string                           `json:"$type,omitempty" cborgen:"$type,omitempty"`
	Thread        *FeedGetPostThread_Output_Thread `json:"thread" cborgen:"thread"`
}

type FeedGetPostThread_Output_Thread struct {
	FeedDefs_ThreadViewPost *FeedDefs_ThreadViewPost
	FeedDefs_NotFoundPost   *FeedDefs_NotFoundPost
}

func (t *FeedGetPostThread_Output_Thread) MarshalJSON() ([]byte, error) {
	if t.FeedDefs_ThreadViewPost != nil {
		t.FeedDefs_ThreadViewPost.LexiconTypeID = "app.bsky.feed.defs#threadViewPost"
		return json.Marshal(t.FeedDefs_ThreadViewPost)
	}
	if t.FeedDefs_NotFoundPost != nil {
		t.FeedDefs_NotFoundPost.LexiconTypeID = "app.bsky.feed.defs#notFoundPost"
		return json.Marshal(t.FeedDefs_NotFoundPost)
	}
	return nil, fmt.Errorf("cannot marshal empty enum")
}
func (t *FeedGetPostThread_Output_Thread) UnmarshalJSON(b []byte) error {
	typ, err := util.TypeExtract(b)
	if err != nil {
		return err
	}

	switch typ {
	case "app.bsky.feed.defs#threadViewPost":
		t.FeedDefs_ThreadViewPost = new(FeedDefs_ThreadViewPost)
		return json.Unmarshal(b, t.FeedDefs_ThreadViewPost)
	case "app.bsky.feed.defs#notFoundPost":
		t.FeedDefs_NotFoundPost = new(FeedDefs_NotFoundPost)
		return json.Unmarshal(b, t.FeedDefs_NotFoundPost)

	default:
		return nil
	}
}

func FeedGetPostThread(ctx context.Context, c *xrpc.Client, depth int64, uri string) (*FeedGetPostThread_Output, error) {
	var out FeedGetPostThread_Output

	params := map[string]interface{}{
		"depth": depth,
		"uri":   uri,
	}
	if err := c.Do(ctx, xrpc.Query, "", "app.bsky.feed.getPostThread", params, nil, &out); err != nil {
		return nil, err
	}

	return &out, nil
}
