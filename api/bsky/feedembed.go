package schemagen

import (
	"encoding/json"
	"fmt"

	"github.com/whyrusleeping/gosky/lex/util"
)

// schema: app.bsky.feed.embed

func init() {
}

type FeedEmbed struct {
	Items []*FeedEmbed_Items_Elem `json:"items" cborgen:"items"`
}

type FeedEmbed_Items_Elem struct {
	FeedEmbed_Media    *FeedEmbed_Media
	FeedEmbed_Record   *FeedEmbed_Record
	FeedEmbed_External *FeedEmbed_External
}

func (t *FeedEmbed_Items_Elem) MarshalJSON() ([]byte, error) {
	if t.FeedEmbed_Media != nil {
		return json.Marshal(t.FeedEmbed_Media)
	}
	if t.FeedEmbed_Record != nil {
		return json.Marshal(t.FeedEmbed_Record)
	}
	if t.FeedEmbed_External != nil {
		return json.Marshal(t.FeedEmbed_External)
	}
	return nil, fmt.Errorf("cannot marshal empty enum")
}
func (t *FeedEmbed_Items_Elem) UnmarshalJSON(b []byte) error {
	typ, err := util.TypeExtract(b)
	if err != nil {
		return err
	}

	switch typ {
	case "app.bsky.feed.embed#media":
		t.FeedEmbed_Media = new(FeedEmbed_Media)
		return json.Unmarshal(b, t.FeedEmbed_Media)
	case "app.bsky.feed.embed#record":
		t.FeedEmbed_Record = new(FeedEmbed_Record)
		return json.Unmarshal(b, t.FeedEmbed_Record)
	case "app.bsky.feed.embed#external":
		t.FeedEmbed_External = new(FeedEmbed_External)
		return json.Unmarshal(b, t.FeedEmbed_External)

	default:
		return nil
	}
}

type FeedEmbed_Media struct {
	Alt      *string    `json:"alt" cborgen:"alt"`
	Thumb    *util.Blob `json:"thumb" cborgen:"thumb"`
	Original *util.Blob `json:"original" cborgen:"original"`
}

type FeedEmbed_Record struct {
	Type   string             `json:"type" cborgen:"type"`
	Author *ActorRef_WithInfo `json:"author" cborgen:"author"`
	Record any                `json:"record" cborgen:"record"`
}

type FeedEmbed_External struct {
	Uri         string `json:"uri" cborgen:"uri"`
	Title       string `json:"title" cborgen:"title"`
	Description string `json:"description" cborgen:"description"`
	ImageUri    string `json:"imageUri" cborgen:"imageUri"`
	Type        string `json:"type" cborgen:"type"`
}
