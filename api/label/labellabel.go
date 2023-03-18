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
	LexiconTypeID string  `json:"$type" cborgen:"$type,const=com.atproto.label.label"`
	Cid           *string `json:"cid,omitempty" cborgen:"cid"`
	Cts           string  `json:"cts" cborgen:"cts"`
	Src           string  `json:"src" cborgen:"src"`
	Uri           string  `json:"uri" cborgen:"uri"`
	Val           string  `json:"val" cborgen:"val"`
}
