package util

import (
	"bytes"
	"encoding/json"
	"io"
)

type typeExtractor struct {
	Type string `json:"$type"`
}

func TypeExtract(b []byte) (string, error) {
	var te typeExtractor
	if err := json.Unmarshal(b, &te); err != nil {
		return "", err
	}

	return te.Type, nil
}

type Blob struct {
	Cid      string `json:"cid"`
	MimeType string `json:"mimeType"`
}

type CborChecker struct {
	Type string `cborgen:"$type"`
}

func CborTypeExtract(b []byte) (string, error) {
	var tcheck CborChecker
	if err := tcheck.UnmarshalCBOR(bytes.NewReader(b)); err != nil {
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
