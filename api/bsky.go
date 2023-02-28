package api

type PostEntity struct {
	Index *TextSlice `json:"index" cborgen:"index"`
	Type  string     `json:"type" cborgen:"type"`
	Value string     `json:"value" cborgen:"value"`
}

type TextSlice struct {
	Start int64 `json:"start" cborgen:"start"`
	End   int64 `json:"end" cborgen:"end"`
}

type ReplyRef struct {
	Root   PostRef `json:"root" cborgen:"root"`
	Parent PostRef `json:"parent" cborgen:"parent"`
}

type PostRecord struct {
	Type      string        `json:"$type,omitempty" cborgen:"$type"`
	Text      string        `json:"text" cborgen:"text"`
	Entities  []*PostEntity `json:"entities,omitempty" cborgen:"entities"`
	Reply     *ReplyRef     `json:"reply,omitempty" cborgen:"reply"`
	CreatedAt string        `json:"createdAt" cborgen:"createdAt"`
}

type PostRef struct {
	Uri string `json:"uri"`
	Cid string `json:"cid"`
}
