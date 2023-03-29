package util

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"

	"github.com/ipfs/go-cid"
)

type typeExtractor struct {
	Type string `json:"$type" cborgen:"$type"`
}

func TypeExtract(b []byte) (string, error) {
	var te typeExtractor
	if err := json.Unmarshal(b, &te); err != nil {
		return "", err
	}

	return te.Type, nil
}

type Blob struct {
	Ref      cid.Cid `json:"ref" cborgen:"ref"`
	MimeType string  `json:"mimeType" cborgen:"mimeType"`
	Size     int64   `json:"size" cborgen:"size"`
}

type CborChecker struct {
	Type string `json:"$type" cborgen:"$type"`
}

func CborTypeExtract(b []byte) (string, error) {
	var tcheck CborChecker
	if err := tcheck.UnmarshalCBOR(bytes.NewReader(b)); err != nil {
		fmt.Printf("bad bytes: %x\n", b)
		return "", err
	}

	return tcheck.Type, nil
}

func CborTypeExtractReader(r io.Reader) (string, []byte, error) {
	buf := new(bytes.Buffer)
	tr := io.TeeReader(r, buf)
	var tcheck CborChecker
	if err := tcheck.UnmarshalCBOR(tr); err != nil {
		return "", nil, err
	}

	return tcheck.Type, buf.Bytes(), nil
}
