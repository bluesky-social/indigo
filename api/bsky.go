package api

import (
	"encoding/json"

	"github.com/whyrusleeping/gosky/xrpc"
)

type BskyApp struct {
	C *xrpc.Client
}

type PostEntity struct {
	Index []int  `json:"index"`
	Type  string `json:"type"`
	Value string `json:"value"`
}

type replyRef struct {
	Root   PostRef `json:"root"`
	Parent PostRef `json:"parent"`
}

type PostRecord struct {
	Text      string      `json:"text"`
	Entities  *PostEntity `json:"entities,omitempty"`
	Reply     *replyRef   `json:"reply,omitempty"`
	CreatedAt string      `json:"createdAt"`
}

func (pr PostRecord) Type() string {
	return "app.bsky.post"
}

type JsonLD interface {
	Type() string
}

type RecordWrapper struct {
	Sub JsonLD
}

func (rw *RecordWrapper) MarshalJSON() ([]byte, error) {
	b, err := json.Marshal(rw.Sub)
	if err != nil {
		return nil, err
	}

	inject := "\"$type\":\"" + rw.Sub.Type() + "\","

	n := append([]byte("{"), []byte(inject)...)
	n = append(n, b[1:]...)

	return n, nil
}

type PostRef struct {
	Uri string `json:"uri"`
	Cid string `json:"cid"`
}
