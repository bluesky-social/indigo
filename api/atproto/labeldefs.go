package atproto

// schema: com.atproto.label.defs

func init() {
}

type LabelDefs_Label struct {
	Cid *string `json:"cid,omitempty" cborgen:"cid,omitempty"`
	Cts string  `json:"cts" cborgen:"cts"`
	Neg *bool   `json:"neg,omitempty" cborgen:"neg,omitempty"`
	Src string  `json:"src" cborgen:"src"`
	Uri string  `json:"uri" cborgen:"uri"`
	Val string  `json:"val" cborgen:"val"`
}
