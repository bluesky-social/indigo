package schemagen

import (
	comatprototypes "github.com/whyrusleeping/gosky/api/atproto"
	"github.com/whyrusleeping/gosky/lex/util"
)

// schema: app.bsky.feed.post

func init() {
	util.RegisterType("app.bsky.feed.post", FeedPost{})
}

// RECORDTYPE: FeedPost
type FeedPost struct {
	LexiconTypeID string             `json:"$type" cborgen:"$type,const=app.bsky.feed.post"`
	Text          string             `json:"text" cborgen:"text"`
	Entities      []*FeedPost_Entity `json:"entities" cborgen:"entities"`
	Reply         *FeedPost_ReplyRef `json:"reply" cborgen:"reply"`
	CreatedAt     string             `json:"createdAt" cborgen:"createdAt"`
}

type FeedPost_ReplyRef struct {
	Root   *comatprototypes.RepoStrongRef `json:"root" cborgen:"root"`
	Parent *comatprototypes.RepoStrongRef `json:"parent" cborgen:"parent"`
}

type FeedPost_Entity struct {
	Index *FeedPost_TextSlice `json:"index" cborgen:"index"`
	Type  string              `json:"type" cborgen:"type"`
	Value string              `json:"value" cborgen:"value"`
}

type FeedPost_TextSlice struct {
	End   int64 `json:"end" cborgen:"end"`
	Start int64 `json:"start" cborgen:"start"`
}
