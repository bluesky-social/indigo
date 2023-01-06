package schemagen

import (
	"github.com/whyrusleeping/gosky/lex/util"
)

// schema: app.bsky.embed.images

func init() {
}

type EmbedImages struct {
	LexiconTypeID string               `json:"$type,omitempty"`
	Images        []*EmbedImages_Image `json:"images" cborgen:"images"`
}

type EmbedImages_Image struct {
	LexiconTypeID string     `json:"$type,omitempty"`
	Alt           string     `json:"alt" cborgen:"alt"`
	Image         *util.Blob `json:"image" cborgen:"image"`
}

type EmbedImages_Presented struct {
	LexiconTypeID string                        `json:"$type,omitempty"`
	Images        []*EmbedImages_PresentedImage `json:"images" cborgen:"images"`
}

type EmbedImages_PresentedImage struct {
	LexiconTypeID string `json:"$type,omitempty"`
	Alt           string `json:"alt" cborgen:"alt"`
	Fullsize      string `json:"fullsize" cborgen:"fullsize"`
	Thumb         string `json:"thumb" cborgen:"thumb"`
}
