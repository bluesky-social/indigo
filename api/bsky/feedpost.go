package schemagen

import (
	"encoding/json"

	comatprototypes "github.com/whyrusleeping/gosky/api/atproto"
)

// schema: app.bsky.feed.post

type FeedPost_Entity struct {
	Index *FeedPost_TextSlice `json:"index" cborgen:"index"`
	Type  string              `json:"type" cborgen:"type"`
	Value string              `json:"value" cborgen:"value"`
}

func (t *FeedPost_Entity) MarshalJSON() ([]byte, error) {
	out := make(map[string]interface{})
	out["index"] = t.Index
	out["type"] = t.Type
	out["value"] = t.Value
	return json.Marshal(out)
}

type FeedPost_TextSlice struct {
	Start int64 `json:"start" cborgen:"start"`
	End   int64 `json:"end" cborgen:"end"`
}

func (t *FeedPost_TextSlice) MarshalJSON() ([]byte, error) {
	out := make(map[string]interface{})
	out["end"] = t.End
	out["start"] = t.Start
	return json.Marshal(out)
}

// RECORDTYPE: FeedPost
type FeedPost struct {
	Text      string             `json:"text" cborgen:"text"`
	Entities  []*FeedPost_Entity `json:"entities" cborgen:"entities"`
	Reply     *FeedPost_ReplyRef `json:"reply" cborgen:"reply"`
	CreatedAt string             `json:"createdAt" cborgen:"createdAt"`
}

func (t *FeedPost) MarshalJSON() ([]byte, error) {
	out := make(map[string]interface{})
	out["createdAt"] = t.CreatedAt
	out["entities"] = t.Entities
	out["reply"] = t.Reply
	out["text"] = t.Text
	return json.Marshal(out)
}

type FeedPost_ReplyRef struct {
	Parent *comatprototypes.RepoStrongRef `json:"parent" cborgen:"parent"`
	Root   *comatprototypes.RepoStrongRef `json:"root" cborgen:"root"`
}

func (t *FeedPost_ReplyRef) MarshalJSON() ([]byte, error) {
	out := make(map[string]interface{})
	out["parent"] = t.Parent
	out["root"] = t.Root
	return json.Marshal(out)
}
