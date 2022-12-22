package schemagen

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"

	cbg "github.com/whyrusleeping/cbor-gen"
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
	CreatedAt     string             `json:"createdAt" cborgen:"createdAt"`
	Embed         *FeedPost_Embed    `json:"embed" cborgen:"embed"`
	Entities      []*FeedPost_Entity `json:"entities" cborgen:"entities"`
	Reply         *FeedPost_ReplyRef `json:"reply" cborgen:"reply"`
	Text          string             `json:"text" cborgen:"text"`
}

type FeedPost_Embed struct {
	EmbedImages   *EmbedImages
	EmbedExternal *EmbedExternal
}

func (t *FeedPost_Embed) MarshalJSON() ([]byte, error) {
	if t.EmbedImages != nil {
		return json.Marshal(t.EmbedImages)
	}
	if t.EmbedExternal != nil {
		return json.Marshal(t.EmbedExternal)
	}
	return nil, fmt.Errorf("cannot marshal empty enum")
}
func (t *FeedPost_Embed) UnmarshalJSON(b []byte) error {
	typ, err := util.TypeExtract(b)
	if err != nil {
		return err
	}

	switch typ {
	case "app.bsky.embed.images":
		t.EmbedImages = new(EmbedImages)
		return json.Unmarshal(b, t.EmbedImages)
	case "app.bsky.embed.external":
		t.EmbedExternal = new(EmbedExternal)
		return json.Unmarshal(b, t.EmbedExternal)

	default:
		return nil
	}
}

func (t *FeedPost_Embed) MarshalCBOR(w io.Writer) error {

	if t == nil {
		_, err := w.Write(cbg.CborNull)
		return err
	}
	if t.EmbedImages != nil {
		return t.EmbedImages.MarshalCBOR(w)
	}
	if t.EmbedExternal != nil {
		return t.EmbedExternal.MarshalCBOR(w)
	}
	return fmt.Errorf("cannot cbor marshal empty enum")
}
func (t *FeedPost_Embed) UnmarshalCBOR(r io.Reader) error {
	typ, b, err := util.CborTypeExtractReader(r)
	if err != nil {
		return err
	}

	switch typ {
	case "app.bsky.embed.images":
		t.EmbedImages = new(EmbedImages)
		return t.EmbedImages.UnmarshalCBOR(bytes.NewReader(b))
	case "app.bsky.embed.external":
		t.EmbedExternal = new(EmbedExternal)
		return t.EmbedExternal.UnmarshalCBOR(bytes.NewReader(b))

	default:
		return nil
	}
}

type FeedPost_ReplyRef struct {
	Parent *comatprototypes.RepoStrongRef `json:"parent" cborgen:"parent"`
	Root   *comatprototypes.RepoStrongRef `json:"root" cborgen:"root"`
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
