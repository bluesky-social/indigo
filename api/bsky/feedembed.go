package schemagen

import (
	"encoding/json"
	"fmt"

	"github.com/whyrusleeping/gosky/lex/util"
)

// schema: app.bsky.feed.embed

type FeedEmbed_External struct {
	Title       string `json:"title" cborgen:"title"`
	Description string `json:"description" cborgen:"description"`
	ImageUri    string `json:"imageUri" cborgen:"imageUri"`
	Type        string `json:"type" cborgen:"type"`
	Uri         string `json:"uri" cborgen:"uri"`
}

func (t *FeedEmbed_External) MarshalJSON() ([]byte, error) {
	t.Type = "external"
	out := make(map[string]interface{})
	out["description"] = t.Description
	out["imageUri"] = t.ImageUri
	out["title"] = t.Title
	out["type"] = t.Type
	out["uri"] = t.Uri
	return json.Marshal(out)
}

type FeedEmbed struct {
	Items []*FeedEmbed_Items_Elem `json:"items" cborgen:"items"`
}

func (t *FeedEmbed) MarshalJSON() ([]byte, error) {
	out := make(map[string]interface{})
	out["items"] = t.Items
	return json.Marshal(out)
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
	typ, err := util.EnumTypeExtract(b)
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
	Alt      string     `json:"alt" cborgen:"alt"`
	Thumb    *util.Blob `json:"thumb" cborgen:"thumb"`
	Original *util.Blob `json:"original" cborgen:"original"`
}

func (t *FeedEmbed_Media) MarshalJSON() ([]byte, error) {
	out := make(map[string]interface{})
	out["alt"] = t.Alt
	out["original"] = t.Original
	out["thumb"] = t.Thumb
	return json.Marshal(out)
}

type FeedEmbed_Record struct {
	Type   string             `json:"type" cborgen:"type"`
	Author *ActorRef_WithInfo `json:"author" cborgen:"author"`
	Record any                `json:"record" cborgen:"record"`
}

func (t *FeedEmbed_Record) MarshalJSON() ([]byte, error) {
	t.Type = "record"
	out := make(map[string]interface{})
	out["author"] = t.Author
	out["record"] = t.Record
	out["type"] = t.Type
	return json.Marshal(out)
}
