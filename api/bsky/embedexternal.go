package bsky

import (
	"github.com/bluesky-social/indigo/lex/util"
)

// schema: app.bsky.embed.external

func init() {
	util.RegisterType("app.bsky.embed.external#main", &EmbedExternal{})
}

// RECORDTYPE: EmbedExternal
type EmbedExternal struct {
	LexiconTypeID string                  `json:"$type,const=app.bsky.embed.external" cborgen:"$type,const=app.bsky.embed.external"`
	External      *EmbedExternal_External `json:"external" cborgen:"external"`
}

type EmbedExternal_External struct {
	Description string        `json:"description" cborgen:"description"`
	Thumb       *util.LexBlob `json:"thumb,omitempty" cborgen:"thumb,omitempty"`
	Title       string        `json:"title" cborgen:"title"`
	Uri         string        `json:"uri" cborgen:"uri"`
}

// RECORDTYPE: EmbedExternal_View
type EmbedExternal_View struct {
	LexiconTypeID string                      `json:"$type,const=app.bsky.embed.external" cborgen:"$type,const=app.bsky.embed.external"`
	External      *EmbedExternal_ViewExternal `json:"external" cborgen:"external"`
}

type EmbedExternal_ViewExternal struct {
	Description string  `json:"description" cborgen:"description"`
	Thumb       *string `json:"thumb,omitempty" cborgen:"thumb,omitempty"`
	Title       string  `json:"title" cborgen:"title"`
	Uri         string  `json:"uri" cborgen:"uri"`
}
