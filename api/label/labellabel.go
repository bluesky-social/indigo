package label

import (
	"github.com/bluesky-social/indigo/lex/util"
)

// schema: com.atproto.label.label

func init() {
	util.RegisterType("com.atproto.label.label", &Label{})
}

// RECORDTYPE: Label
type Label struct {
	LexiconTypeID string  `json:"$type,const=com.atproto.label.label" cborgen:"$type,const=com.atproto.label.label"`
	Cid           *string `json:"cid,omitempty" cborgen:"cid,omitempty"`
	Cts           string  `json:"cts" cborgen:"cts"`
	// manually setting this to 'bool' not '*bool'
	Neg bool   `json:"neg" cborgen:"neg"`
	Src string `json:"src" cborgen:"src"`
	Uri string `json:"uri" cborgen:"uri"`
	Val string `json:"val" cborgen:"val"`
}
