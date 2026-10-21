package util

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
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

type UnknownType struct {
	JSON json.RawMessage
}

func (t *UnknownType) MarshalJSON() ([]byte, error) {
	return t.JSON.MarshalJSON()
}

func (t *UnknownType) UnmarshalJSON(b []byte) error {
	return t.JSON.UnmarshalJSON(b)
}

func (t *UnknownType) MarshalCBOR(w io.Writer) error {
	return fmt.Errorf("converting unrecognized types from JSON to CBOR is not implemented")
}
