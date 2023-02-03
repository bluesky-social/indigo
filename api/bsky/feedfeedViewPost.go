package bsky

import (
	"encoding/json"
	"fmt"

	"github.com/bluesky-social/indigo/lex/util"
)

// schema: app.bsky.feed.feedViewPost

func init() {
}

type FeedFeedViewPost struct {
	LexiconTypeID string                     `json:"$type,omitempty"`
	Post          *FeedPost_View             `json:"post" cborgen:"post"`
	Reason        *FeedFeedViewPost_Reason   `json:"reason,omitempty" cborgen:"reason"`
	Reply         *FeedFeedViewPost_ReplyRef `json:"reply,omitempty" cborgen:"reply"`
}

type FeedFeedViewPost_Reason struct {
	FeedFeedViewPost_ReasonRepost *FeedFeedViewPost_ReasonRepost
}

func (t *FeedFeedViewPost_Reason) MarshalJSON() ([]byte, error) {
	if t.FeedFeedViewPost_ReasonRepost != nil {
		t.FeedFeedViewPost_ReasonRepost.LexiconTypeID = "app.bsky.feed.feedViewPost#reasonRepost"
		return json.Marshal(t.FeedFeedViewPost_ReasonRepost)
	}
	return nil, fmt.Errorf("cannot marshal empty enum")
}
func (t *FeedFeedViewPost_Reason) UnmarshalJSON(b []byte) error {
	typ, err := util.TypeExtract(b)
	if err != nil {
		return err
	}

	switch typ {
	case "app.bsky.feed.feedViewPost#reasonRepost":
		t.FeedFeedViewPost_ReasonRepost = new(FeedFeedViewPost_ReasonRepost)
		return json.Unmarshal(b, t.FeedFeedViewPost_ReasonRepost)

	default:
		return nil
	}
}

type FeedFeedViewPost_ReasonRepost struct {
	LexiconTypeID string             `json:"$type,omitempty"`
	By            *ActorRef_WithInfo `json:"by" cborgen:"by"`
	IndexedAt     string             `json:"indexedAt" cborgen:"indexedAt"`
}

type FeedFeedViewPost_ReplyRef struct {
	LexiconTypeID string         `json:"$type,omitempty"`
	Parent        *FeedPost_View `json:"parent" cborgen:"parent"`
	Root          *FeedPost_View `json:"root" cborgen:"root"`
}
