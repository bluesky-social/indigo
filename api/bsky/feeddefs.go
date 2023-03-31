package bsky

import (
	"encoding/json"
	"fmt"

	"github.com/bluesky-social/indigo/lex/util"
)

// schema: app.bsky.feed.defs

func init() {
}

type FeedDefs_FeedViewPost struct {
	LexiconTypeID string                        `json:"$type,omitempty" cborgen:"$type,omitempty"`
	Post          *FeedDefs_PostView            `json:"post" cborgen:"post"`
	Reason        *FeedDefs_FeedViewPost_Reason `json:"reason,omitempty" cborgen:"reason,omitempty"`
	Reply         *FeedDefs_ReplyRef            `json:"reply,omitempty" cborgen:"reply,omitempty"`
}

type FeedDefs_FeedViewPost_Reason struct {
	FeedDefs_ReasonRepost *FeedDefs_ReasonRepost
}

func (t *FeedDefs_FeedViewPost_Reason) MarshalJSON() ([]byte, error) {
	if t.FeedDefs_ReasonRepost != nil {
		t.FeedDefs_ReasonRepost.LexiconTypeID = "app.bsky.feed.defs#reasonRepost"
		return json.Marshal(t.FeedDefs_ReasonRepost)
	}
	return nil, fmt.Errorf("cannot marshal empty enum")
}
func (t *FeedDefs_FeedViewPost_Reason) UnmarshalJSON(b []byte) error {
	typ, err := util.TypeExtract(b)
	if err != nil {
		return err
	}

	switch typ {
	case "app.bsky.feed.defs#reasonRepost":
		t.FeedDefs_ReasonRepost = new(FeedDefs_ReasonRepost)
		return json.Unmarshal(b, t.FeedDefs_ReasonRepost)

	default:
		return nil
	}
}

type FeedDefs_NotFoundPost struct {
	LexiconTypeID string `json:"$type,omitempty" cborgen:"$type,omitempty"`
	NotFound      bool   `json:"notFound" cborgen:"notFound"`
	Uri           string `json:"uri" cborgen:"uri"`
}

type FeedDefs_PostView struct {
	LexiconTypeID string                      `json:"$type,omitempty" cborgen:"$type,omitempty"`
	Author        *ActorDefs_ProfileViewBasic `json:"author" cborgen:"author"`
	Cid           string                      `json:"cid" cborgen:"cid"`
	Embed         *FeedDefs_PostView_Embed    `json:"embed,omitempty" cborgen:"embed,omitempty"`
	IndexedAt     string                      `json:"indexedAt" cborgen:"indexedAt"`
	LikeCount     *int64                      `json:"likeCount,omitempty" cborgen:"likeCount,omitempty"`
	Record        *util.LexiconTypeDecoder    `json:"record" cborgen:"record"`
	ReplyCount    *int64                      `json:"replyCount,omitempty" cborgen:"replyCount,omitempty"`
	RepostCount   *int64                      `json:"repostCount,omitempty" cborgen:"repostCount,omitempty"`
	Uri           string                      `json:"uri" cborgen:"uri"`
	Viewer        *FeedDefs_ViewerState       `json:"viewer,omitempty" cborgen:"viewer,omitempty"`
}

type FeedDefs_PostView_Embed struct {
	EmbedImages_View          *EmbedImages_View
	EmbedExternal_View        *EmbedExternal_View
	EmbedRecord_View          *EmbedRecord_View
	EmbedRecordWithMedia_View *EmbedRecordWithMedia_View
}

func (t *FeedDefs_PostView_Embed) MarshalJSON() ([]byte, error) {
	if t.EmbedImages_View != nil {
		t.EmbedImages_View.LexiconTypeID = "app.bsky.embed.images#view"
		return json.Marshal(t.EmbedImages_View)
	}
	if t.EmbedExternal_View != nil {
		t.EmbedExternal_View.LexiconTypeID = "app.bsky.embed.external#view"
		return json.Marshal(t.EmbedExternal_View)
	}
	if t.EmbedRecord_View != nil {
		t.EmbedRecord_View.LexiconTypeID = "app.bsky.embed.record#view"
		return json.Marshal(t.EmbedRecord_View)
	}
	if t.EmbedRecordWithMedia_View != nil {
		t.EmbedRecordWithMedia_View.LexiconTypeID = "app.bsky.embed.recordWithMedia#view"
		return json.Marshal(t.EmbedRecordWithMedia_View)
	}
	return nil, fmt.Errorf("cannot marshal empty enum")
}
func (t *FeedDefs_PostView_Embed) UnmarshalJSON(b []byte) error {
	typ, err := util.TypeExtract(b)
	if err != nil {
		return err
	}

	switch typ {
	case "app.bsky.embed.images#view":
		t.EmbedImages_View = new(EmbedImages_View)
		return json.Unmarshal(b, t.EmbedImages_View)
	case "app.bsky.embed.external#view":
		t.EmbedExternal_View = new(EmbedExternal_View)
		return json.Unmarshal(b, t.EmbedExternal_View)
	case "app.bsky.embed.record#view":
		t.EmbedRecord_View = new(EmbedRecord_View)
		return json.Unmarshal(b, t.EmbedRecord_View)
	case "app.bsky.embed.recordWithMedia#view":
		t.EmbedRecordWithMedia_View = new(EmbedRecordWithMedia_View)
		return json.Unmarshal(b, t.EmbedRecordWithMedia_View)

	default:
		return nil
	}
}

type FeedDefs_ReasonRepost struct {
	LexiconTypeID string                      `json:"$type,omitempty" cborgen:"$type,omitempty"`
	By            *ActorDefs_ProfileViewBasic `json:"by" cborgen:"by"`
	IndexedAt     string                      `json:"indexedAt" cborgen:"indexedAt"`
}

type FeedDefs_ReplyRef struct {
	LexiconTypeID string             `json:"$type,omitempty" cborgen:"$type,omitempty"`
	Parent        *FeedDefs_PostView `json:"parent" cborgen:"parent"`
	Root          *FeedDefs_PostView `json:"root" cborgen:"root"`
}

type FeedDefs_ThreadViewPost struct {
	LexiconTypeID string                                  `json:"$type,omitempty" cborgen:"$type,omitempty"`
	Parent        *FeedDefs_ThreadViewPost_Parent         `json:"parent,omitempty" cborgen:"parent,omitempty"`
	Post          *FeedDefs_PostView                      `json:"post" cborgen:"post"`
	Replies       []*FeedDefs_ThreadViewPost_Replies_Elem `json:"replies,omitempty" cborgen:"replies,omitempty"`
}

type FeedDefs_ThreadViewPost_Parent struct {
	FeedDefs_ThreadViewPost *FeedDefs_ThreadViewPost
	FeedDefs_NotFoundPost   *FeedDefs_NotFoundPost
}

func (t *FeedDefs_ThreadViewPost_Parent) MarshalJSON() ([]byte, error) {
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
func (t *FeedDefs_ThreadViewPost_Parent) UnmarshalJSON(b []byte) error {
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

type FeedDefs_ThreadViewPost_Replies_Elem struct {
	FeedDefs_ThreadViewPost *FeedDefs_ThreadViewPost
	FeedDefs_NotFoundPost   *FeedDefs_NotFoundPost
}

func (t *FeedDefs_ThreadViewPost_Replies_Elem) MarshalJSON() ([]byte, error) {
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
func (t *FeedDefs_ThreadViewPost_Replies_Elem) UnmarshalJSON(b []byte) error {
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

type FeedDefs_ViewerState struct {
	LexiconTypeID string  `json:"$type,omitempty" cborgen:"$type,omitempty"`
	Like          *string `json:"like,omitempty" cborgen:"like,omitempty"`
	Repost        *string `json:"repost,omitempty" cborgen:"repost,omitempty"`
}
