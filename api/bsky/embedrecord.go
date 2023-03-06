package bsky

import (
	"encoding/json"
	"fmt"

	comatprototypes "github.com/bluesky-social/indigo/api/atproto"
	"github.com/bluesky-social/indigo/lex/util"
)

// schema: app.bsky.embed.record

func init() {
}

type EmbedRecord struct {
	LexiconTypeID string                         `json:"$type,omitempty"`
	Record        *comatprototypes.RepoStrongRef `json:"record" cborgen:"record"`
}

type EmbedRecord_Presented struct {
	LexiconTypeID string                        `json:"$type,omitempty"`
	Record        *EmbedRecord_Presented_Record `json:"record" cborgen:"record"`
}

type EmbedRecord_PresentedNotFound struct {
	LexiconTypeID string `json:"$type,omitempty"`
	Uri           string `json:"uri" cborgen:"uri"`
}

type EmbedRecord_PresentedRecord struct {
	LexiconTypeID string                  `json:"$type,omitempty"`
	Author        *ActorRef_WithInfo      `json:"author" cborgen:"author"`
	Cid           string                  `json:"cid" cborgen:"cid"`
	Record        util.LexiconTypeDecoder `json:"record" cborgen:"record"`
	Uri           string                  `json:"uri" cborgen:"uri"`
}

type EmbedRecord_Presented_Record struct {
	EmbedRecord_PresentedRecord   *EmbedRecord_PresentedRecord
	EmbedRecord_PresentedNotFound *EmbedRecord_PresentedNotFound
}

func (t *EmbedRecord_Presented_Record) MarshalJSON() ([]byte, error) {
	if t.EmbedRecord_PresentedRecord != nil {
		t.EmbedRecord_PresentedRecord.LexiconTypeID = "app.bsky.embed.record#presentedRecord"
		return json.Marshal(t.EmbedRecord_PresentedRecord)
	}
	if t.EmbedRecord_PresentedNotFound != nil {
		t.EmbedRecord_PresentedNotFound.LexiconTypeID = "app.bsky.embed.record#presentedNotFound"
		return json.Marshal(t.EmbedRecord_PresentedNotFound)
	}
	return nil, fmt.Errorf("cannot marshal empty enum")
}
func (t *EmbedRecord_Presented_Record) UnmarshalJSON(b []byte) error {
	typ, err := util.TypeExtract(b)
	if err != nil {
		return err
	}

	switch typ {
	case "app.bsky.embed.record#presentedRecord":
		t.EmbedRecord_PresentedRecord = new(EmbedRecord_PresentedRecord)
		return json.Unmarshal(b, t.EmbedRecord_PresentedRecord)
	case "app.bsky.embed.record#presentedNotFound":
		t.EmbedRecord_PresentedNotFound = new(EmbedRecord_PresentedNotFound)
		return json.Unmarshal(b, t.EmbedRecord_PresentedNotFound)

	default:
		return nil
	}
}
