package data

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"

	cbg "github.com/whyrusleeping/cbor-gen"
)

// Represents the "bytes" type from the atproto data model.
//
// In JSON, marshals to an object with $bytes key and base64-encoded data.
//
// In CBOR, marshals to a byte array.
type Bytes []byte

type JsonBytes struct {
	Bytes string `json:"$bytes"`
}

func (lb Bytes) MarshalJSON() ([]byte, error) {
	if lb == nil {
		return nil, fmt.Errorf("tried to marshal nil $bytes")
	}
	jb := JsonBytes{
		Bytes: base64.RawStdEncoding.EncodeToString([]byte(lb)),
	}
	return json.Marshal(jb)
}

func (lb *Bytes) UnmarshalJSON(raw []byte) error {
	var jb JsonBytes
	err := json.Unmarshal(raw, &jb)
	if err != nil {
		return fmt.Errorf("parsing $bytes JSON: %v", err)
	}
	out, err := base64.RawStdEncoding.DecodeString(jb.Bytes)
	if err != nil {
		return fmt.Errorf("parsing $bytes base64: %v", err)
	}
	*lb = Bytes(out)
	return nil
}

func (lb *Bytes) MarshalCBOR(w io.Writer) error {
	if lb == nil {
		_, err := w.Write(cbg.CborNull)
		return err
	}
	cw := cbg.NewCborWriter(w)
	if err := cbg.WriteByteArray(cw, ([]byte)(*lb)); err != nil {
		return fmt.Errorf("failed to write $bytes as CBOR: %w", err)
	}
	return nil
}

func (lb *Bytes) UnmarshalCBOR(r io.Reader) error {
	cr := cbg.NewCborReader(r)
	b, err := cbg.ReadByteArray(cr, MAX_RECORD_BYTES_LEN)
	if err != nil {
		return fmt.Errorf("failed to read $bytes from CBOR: %w", err)
	}
	*lb = Bytes(b)
	return nil
}
