package schemagen

import (
	"encoding/json"

	comatprototypes "github.com/whyrusleeping/gosky/api/atproto"
)

// schema: app.bsky.feed.post

type FeedPost struct {
	Entities  []*FeedPost_Entity `json:"entities"`
	Reply     *FeedPost_ReplyRef `json:"reply"`
	CreatedAt string             `json:"createdAt"`
	Text      string             `json:"text"`
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
	Root   *comatprototypes.RepoStrongRef `json:"root"`
	Parent *comatprototypes.RepoStrongRef `json:"parent"`
}

func (t *FeedPost_ReplyRef) MarshalJSON() ([]byte, error) {
	out := make(map[string]interface{})
	out["parent"] = t.Parent
	out["root"] = t.Root
	return json.Marshal(out)
}

type FeedPost_Entity struct {
	Index *FeedPost_TextSlice `json:"index"`
	Type  string              `json:"type"`
	Value string              `json:"value"`
}

func (t *FeedPost_Entity) MarshalJSON() ([]byte, error) {
	out := make(map[string]interface{})
	out["index"] = t.Index
	out["type"] = t.Type
	out["value"] = t.Value
	return json.Marshal(out)
}

type FeedPost_TextSlice struct {
	End   int64 `json:"end"`
	Start int64 `json:"start"`
}

func (t *FeedPost_TextSlice) MarshalJSON() ([]byte, error) {
	out := make(map[string]interface{})
	out["end"] = t.End
	out["start"] = t.Start
	return json.Marshal(out)
}
