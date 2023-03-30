package bsky

import (
	"encoding/json"
	"fmt"

	"github.com/bluesky-social/indigo/lex/util"
)

// schema: app.bsky.richtext.facet

func init() {
}

type RichtextFacet struct {
	LexiconTypeID string                         `json:"$type,omitempty" cborgen:"$type,omitempty"`
	Features      []*RichtextFacet_Features_Elem `json:"features" cborgen:"features"`
	Index         *RichtextFacet_ByteSlice       `json:"index" cborgen:"index"`
}

type RichtextFacet_ByteSlice struct {
	LexiconTypeID string `json:"$type,omitempty" cborgen:"$type,omitempty"`
	ByteEnd       int64  `json:"byteEnd" cborgen:"byteEnd"`
	ByteStart     int64  `json:"byteStart" cborgen:"byteStart"`
}

type RichtextFacet_Features_Elem struct {
	RichtextFacet_Mention *RichtextFacet_Mention
	RichtextFacet_Link    *RichtextFacet_Link
}

func (t *RichtextFacet_Features_Elem) MarshalJSON() ([]byte, error) {
	if t.RichtextFacet_Mention != nil {
		t.RichtextFacet_Mention.LexiconTypeID = "app.bsky.richtext.facet#mention"
		return json.Marshal(t.RichtextFacet_Mention)
	}
	if t.RichtextFacet_Link != nil {
		t.RichtextFacet_Link.LexiconTypeID = "app.bsky.richtext.facet#link"
		return json.Marshal(t.RichtextFacet_Link)
	}
	return nil, fmt.Errorf("cannot marshal empty enum")
}
func (t *RichtextFacet_Features_Elem) UnmarshalJSON(b []byte) error {
	typ, err := util.TypeExtract(b)
	if err != nil {
		return err
	}

	switch typ {
	case "app.bsky.richtext.facet#mention":
		t.RichtextFacet_Mention = new(RichtextFacet_Mention)
		return json.Unmarshal(b, t.RichtextFacet_Mention)
	case "app.bsky.richtext.facet#link":
		t.RichtextFacet_Link = new(RichtextFacet_Link)
		return json.Unmarshal(b, t.RichtextFacet_Link)

	default:
		return nil
	}
}

type RichtextFacet_Link struct {
	LexiconTypeID string `json:"$type,omitempty" cborgen:"$type,omitempty"`
	Uri           string `json:"uri" cborgen:"uri"`
}

type RichtextFacet_Mention struct {
	LexiconTypeID string `json:"$type,omitempty" cborgen:"$type,omitempty"`
	Did           string `json:"did" cborgen:"did"`
}
