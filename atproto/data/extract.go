package data

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
)

// Helper type for extracting record $type from CBOR
type GenericRecord struct {
	Type string `json:"$type" cborgen:"$type"`
}

// Parses the top-level $type field from generic atproto JSON data
func ExtractTypeJSON(b []byte) (string, error) {
	var gr GenericRecord
	if err := json.Unmarshal(b, &gr); err != nil {
		return "", err
	}

	return gr.Type, nil
}

// Parses the top-level $type field from generic atproto CBOR data
func ExtractTypeCBOR(b []byte) (string, error) {
	var gr GenericRecord
	if err := gr.UnmarshalCBOR(bytes.NewReader(b)); err != nil {
		fmt.Printf("bad bytes: %x\n", b)
		return "", err
	}

	return gr.Type, nil
}

// Parses top-level $type field from generic atproto CBOR.
//
// Returns that string field, and additional bytes (TODO: the parsed bytes, or remaining bytes?)
func ExtractTypeCBORReader(r io.Reader) (string, []byte, error) {
	buf := new(bytes.Buffer)
	tr := io.TeeReader(r, buf)
	var gr GenericRecord
	if err := gr.UnmarshalCBOR(tr); err != nil {
		return "", nil, err
	}

	return gr.Type, buf.Bytes(), nil
}
