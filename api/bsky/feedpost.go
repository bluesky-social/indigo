package bsky

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"

	comatprototypes "github.com/bluesky-social/indigo/api/atproto"
	"github.com/bluesky-social/indigo/lex/util"
	cbg "github.com/whyrusleeping/cbor-gen"
)

// schema: app.bsky.feed.post

func init() {
	util.RegisterType("app.bsky.feed.post", &FeedPost{})
}

// RECORDTYPE: FeedPost
type FeedPost struct {
	LexiconTypeID string             `json:"$type" cborgen:"$type,const=app.bsky.feed.post"`
	CreatedAt     string             `json:"createdAt" cborgen:"createdAt"`
	Embed         *FeedPost_Embed    `json:"embed,omitempty" cborgen:"embed,omitempty"`
	Entities      []*FeedPost_Entity `json:"entities,omitempty" cborgen:"entities,omitempty"`
	Facets        []*RichtextFacet   `json:"facets,omitempty" cborgen:"facets,omitempty"`
	Reply         *FeedPost_ReplyRef `json:"reply,omitempty" cborgen:"reply,omitempty"`
	Text          string             `json:"text" cborgen:"text"`
}

type FeedPost_Embed struct {
	EmbedImages          *EmbedImages
	EmbedExternal        *EmbedExternal
	EmbedRecord          *EmbedRecord
	EmbedRecordWithMedia *EmbedRecordWithMedia
}

func (t *FeedPost_Embed) MarshalJSON() ([]byte, error) {
	if t.EmbedImages != nil {
		t.EmbedImages.LexiconTypeID = "app.bsky.embed.images"
		return json.Marshal(t.EmbedImages)
	}
	if t.EmbedExternal != nil {
		t.EmbedExternal.LexiconTypeID = "app.bsky.embed.external"
		return json.Marshal(t.EmbedExternal)
	}
	if t.EmbedRecord != nil {
		t.EmbedRecord.LexiconTypeID = "app.bsky.embed.record"
		return json.Marshal(t.EmbedRecord)
	}
	if t.EmbedRecordWithMedia != nil {
		t.EmbedRecordWithMedia.LexiconTypeID = "app.bsky.embed.recordWithMedia"
		return json.Marshal(t.EmbedRecordWithMedia)
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
	case "app.bsky.embed.record":
		t.EmbedRecord = new(EmbedRecord)
		return json.Unmarshal(b, t.EmbedRecord)
	case "app.bsky.embed.recordWithMedia":
		t.EmbedRecordWithMedia = new(EmbedRecordWithMedia)
		return json.Unmarshal(b, t.EmbedRecordWithMedia)

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
	if t.EmbedRecord != nil {
		return t.EmbedRecord.MarshalCBOR(w)
	}
	if t.EmbedRecordWithMedia != nil {
		return t.EmbedRecordWithMedia.MarshalCBOR(w)
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
	case "app.bsky.embed.record":
		t.EmbedRecord = new(EmbedRecord)
		return t.EmbedRecord.UnmarshalCBOR(bytes.NewReader(b))
	case "app.bsky.embed.recordWithMedia":
		t.EmbedRecordWithMedia = new(EmbedRecordWithMedia)
		return t.EmbedRecordWithMedia.UnmarshalCBOR(bytes.NewReader(b))

	default:
		return nil
	}
}

type FeedPost_Entity struct {
	LexiconTypeID string              `json:"$type,omitempty" cborgen:"$type,omitempty"`
	Index         *FeedPost_TextSlice `json:"index" cborgen:"index"`
	Type          string              `json:"type" cborgen:"type"`
	Value         string              `json:"value" cborgen:"value"`
}

type FeedPost_ReplyRef struct {
	LexiconTypeID string                         `json:"$type,omitempty" cborgen:"$type,omitempty"`
	Parent        *comatprototypes.RepoStrongRef `json:"parent" cborgen:"parent"`
	Root          *comatprototypes.RepoStrongRef `json:"root" cborgen:"root"`
}

type FeedPost_TextSlice struct {
	LexiconTypeID string `json:"$type,omitempty" cborgen:"$type,omitempty"`
	End           int64  `json:"end" cborgen:"end"`
	Start         int64  `json:"start" cborgen:"start"`
}
