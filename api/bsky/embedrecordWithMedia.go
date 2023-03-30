package bsky

import (
	"encoding/json"
	"fmt"

	"github.com/bluesky-social/indigo/lex/util"
)

// schema: app.bsky.embed.recordWithMedia

func init() {
}

type EmbedRecordWithMedia struct {
	LexiconTypeID string                      `json:"$type,omitempty" cborgen:"$type,omitempty"`
	Media         *EmbedRecordWithMedia_Media `json:"media" cborgen:"media"`
	Record        *EmbedRecord                `json:"record" cborgen:"record"`
}

type EmbedRecordWithMedia_Media struct {
	EmbedImages   *EmbedImages
	EmbedExternal *EmbedExternal
}

func (t *EmbedRecordWithMedia_Media) MarshalJSON() ([]byte, error) {
	if t.EmbedImages != nil {
		t.EmbedImages.LexiconTypeID = "app.bsky.embed.images"
		return json.Marshal(t.EmbedImages)
	}
	if t.EmbedExternal != nil {
		t.EmbedExternal.LexiconTypeID = "app.bsky.embed.external"
		return json.Marshal(t.EmbedExternal)
	}
	return nil, fmt.Errorf("cannot marshal empty enum")
}
func (t *EmbedRecordWithMedia_Media) UnmarshalJSON(b []byte) error {
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

type EmbedRecordWithMedia_View struct {
	LexiconTypeID string                           `json:"$type,omitempty" cborgen:"$type,omitempty"`
	Media         *EmbedRecordWithMedia_View_Media `json:"media" cborgen:"media"`
	Record        *EmbedRecord_View                `json:"record" cborgen:"record"`
}

type EmbedRecordWithMedia_View_Media struct {
	EmbedImages_View   *EmbedImages_View
	EmbedExternal_View *EmbedExternal_View
}

func (t *EmbedRecordWithMedia_View_Media) MarshalJSON() ([]byte, error) {
	if t.EmbedImages_View != nil {
		t.EmbedImages_View.LexiconTypeID = "app.bsky.embed.images#view"
		return json.Marshal(t.EmbedImages_View)
	}
	if t.EmbedExternal_View != nil {
		t.EmbedExternal_View.LexiconTypeID = "app.bsky.embed.external#view"
		return json.Marshal(t.EmbedExternal_View)
	}
	return nil, fmt.Errorf("cannot marshal empty enum")
}
func (t *EmbedRecordWithMedia_View_Media) UnmarshalJSON(b []byte) error {
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

	default:
		return nil
	}
}
