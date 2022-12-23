package schemagen

import (
	"github.com/whyrusleeping/gosky/lex/util"
)

// schema: app.bsky.embed.external

func init() {
}

type EmbedExternal struct {
	External *EmbedExternal_External `json:"external" cborgen:"external"`
}

type EmbedExternal_External struct {
	Description string     `json:"description" cborgen:"description"`
	Thumb       *util.Blob `json:"thumb" cborgen:"thumb"`
	Title       string     `json:"title" cborgen:"title"`
	Uri         string     `json:"uri" cborgen:"uri"`
}

type EmbedExternal_Presented struct {
	External *EmbedExternal_PresentedExternal `json:"external" cborgen:"external"`
}

type EmbedExternal_PresentedExternal struct {
	Description string  `json:"description" cborgen:"description"`
	Thumb       *string `json:"thumb" cborgen:"thumb"`
	Title       string  `json:"title" cborgen:"title"`
	Uri         string  `json:"uri" cborgen:"uri"`
}
