package bsky

import (
	"github.com/bluesky-social/indigo/lex/util"
)

// schema: app.bsky.embed.external

func init() {
}

type EmbedExternal struct {
	LexiconTypeID string                  `json:"$type,omitempty"`
	External      *EmbedExternal_External `json:"external" cborgen:"external"`
}

type EmbedExternal_External struct {
	LexiconTypeID string     `json:"$type,omitempty"`
	Description   string     `json:"description" cborgen:"description"`
	Thumb         *util.Blob `json:"thumb,omitempty" cborgen:"thumb"`
	Title         string     `json:"title" cborgen:"title"`
	Uri           string     `json:"uri" cborgen:"uri"`
}

type EmbedExternal_View struct {
	LexiconTypeID string                      `json:"$type,omitempty"`
	External      *EmbedExternal_ViewExternal `json:"external" cborgen:"external"`
}

type EmbedExternal_ViewExternal struct {
	LexiconTypeID string  `json:"$type,omitempty"`
	Description   string  `json:"description" cborgen:"description"`
	Thumb         *string `json:"thumb,omitempty" cborgen:"thumb"`
	Title         string  `json:"title" cborgen:"title"`
	Uri           string  `json:"uri" cborgen:"uri"`
}
