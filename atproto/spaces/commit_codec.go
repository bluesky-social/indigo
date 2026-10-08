package spaces

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"

	"github.com/bluesky-social/indigo/atproto/atdata"
	"github.com/bluesky-social/indigo/atproto/syntax"
	cbg "github.com/whyrusleeping/cbor-gen"
)

// DecodeCommitJSON decodes a signed protocol commit, including the atproto
// {"$bytes": "..."} representation (un-padded standard base64). It validates
// required fields, version, revision, and byte lengths, but not authenticity.
func DecodeCommitJSON(data []byte) (*Commit, error) {
	var c Commit
	if err := c.UnmarshalJSON(data); err != nil {
		return nil, err
	}
	return &c, nil
}

// MarshalJSON encodes the commit with protocol $bytes objects, never JSON
// arrays or ordinary base64 strings. Unsigned or malformed commits are rejected.
func (c Commit) MarshalJSON() ([]byte, error) {
	if err := c.validate(); err != nil {
		return nil, err
	}
	return json.Marshal(struct {
		Version   int64        `json:"ver"`
		Hash      atdata.Bytes `json:"hash"`
		IKM       atdata.Bytes `json:"ikm"`
		MAC       atdata.Bytes `json:"mac"`
		Signature atdata.Bytes `json:"sig"`
		Revision  syntax.TID   `json:"rev"`
	}{c.Version, c.Hash[:], c.IKM[:], c.MAC[:], c.Signature, c.Revision})
}

// UnmarshalJSON decodes and validates a signed commit. On error the receiver
// is unchanged. An optional $type must name com.atproto.space.defs#signedCommit.
func (c *Commit) UnmarshalJSON(data []byte) error {
	if len(data) > atdata.MAX_JSON_RECORD_SIZE {
		return fmt.Errorf("Spaces commit JSON exceeds maximum size")
	}
	dec := json.NewDecoder(bytes.NewReader(data))
	token, err := dec.Token()
	if err != nil {
		return err
	}
	if token != json.Delim('{') {
		return fmt.Errorf("Spaces commit must be a JSON object")
	}
	var out Commit
	var seen uint8
	for dec.More() {
		token, err := dec.Token()
		if err != nil {
			return err
		}
		field, ok := token.(string)
		if !ok {
			return fmt.Errorf("invalid Spaces commit field")
		}
		var bit uint8
		switch field {
		case "ver":
			bit = 1
		case "hash":
			bit = 2
		case "ikm":
			bit = 4
		case "mac":
			bit = 8
		case "sig":
			bit = 16
		case "rev":
			bit = 32
		case "$type":
			bit = 64
		default:
			return fmt.Errorf("unknown Spaces commit field: %q", field)
		}
		if seen&bit != 0 {
			return fmt.Errorf("duplicate Spaces commit field: %q", field)
		}
		seen |= bit
		switch field {
		case "ver":
			var value *int64
			if err := dec.Decode(&value); err != nil {
				return err
			}
			if value == nil {
				return fmt.Errorf("missing Spaces commit version")
			}
			out.Version = *value
		case "rev", "$type":
			var value *string
			if err := dec.Decode(&value); err != nil {
				return err
			}
			if value == nil {
				return fmt.Errorf("missing Spaces commit %s", field)
			}
			if field == "$type" {
				if *value != commitType {
					return fmt.Errorf("invalid Spaces commit $type")
				}
			} else {
				out.Revision = syntax.TID(*value)
			}
		default:
			var raw json.RawMessage
			if err := dec.Decode(&raw); err != nil {
				return err
			}
			var obj map[string]json.RawMessage
			if err := json.Unmarshal(raw, &obj); err != nil {
				return err
			}
			if len(obj) != 1 || obj["$bytes"] == nil {
				return fmt.Errorf("Spaces commit %s must be a $bytes object", field)
			}
			var value atdata.Bytes
			if err := value.UnmarshalJSON(raw); err != nil {
				return err
			}
			size := 32
			if field == "sig" {
				size = 64
			}
			if len(value) != size {
				return fmt.Errorf("Spaces commit %s must be %d bytes", field, size)
			}
			switch field {
			case "hash":
				copy(out.Hash[:], value)
			case "ikm":
				copy(out.IKM[:], value)
			case "mac":
				copy(out.MAC[:], value)
			case "sig":
				out.Signature = value
			}
		}
	}
	if _, err := dec.Token(); err != nil {
		return err
	}
	if seen&63 != 63 {
		return fmt.Errorf("missing required Spaces commit fields")
	}
	if _, err := dec.Token(); err != io.EOF {
		return fmt.Errorf("trailing data after Spaces commit JSON")
	}
	if err := out.validate(); err != nil {
		return err
	}
	*c = out
	return nil
}

// DecodeCommitCBOR decodes exactly one canonical DAG-CBOR signed commit and
// rejects trailing data. It validates structure, not its signature or MAC.
func DecodeCommitCBOR(data []byte) (*Commit, error) {
	if len(data) > atdata.MAX_CBOR_RECORD_SIZE {
		return nil, fmt.Errorf("Spaces commit CBOR exceeds maximum size")
	}
	r := bytes.NewReader(data)
	var c Commit
	if err := c.UnmarshalCBOR(r); err != nil {
		return nil, err
	}
	if r.Len() != 0 {
		return nil, fmt.Errorf("trailing data after Spaces commit CBOR")
	}
	return &c, nil
}

// MarshalCBOR writes a canonical DAG-CBOR commit with byte strings for all
// binary fields. Unsigned or malformed commits are rejected.
func (c *Commit) MarshalCBOR(w io.Writer) error {
	if err := c.validate(); err != nil {
		return err
	}
	cw := cbg.NewCborWriter(w)
	if err := cw.WriteMajorTypeHeader(cbg.MajMap, 6); err != nil {
		return err
	}
	// Canonical map order: shortest key first, then bytewise.
	for _, field := range []string{"ikm", "mac", "rev", "sig", "ver", "hash"} {
		if err := writeCBORString(cw, field); err != nil {
			return err
		}
		var err error
		switch field {
		case "ikm":
			err = cbg.WriteByteArray(cw, c.IKM[:])
		case "mac":
			err = cbg.WriteByteArray(cw, c.MAC[:])
		case "rev":
			err = writeCBORString(cw, c.Revision.String())
		case "sig":
			err = cbg.WriteByteArray(cw, c.Signature)
		case "ver":
			err = cw.WriteMajorTypeHeader(cbg.MajUnsignedInt, uint64(c.Version))
		case "hash":
			err = cbg.WriteByteArray(cw, c.Hash[:])
		}
		if err != nil {
			return err
		}
	}
	return nil
}

// UnmarshalCBOR reads one canonical DAG-CBOR commit from r without taking
// ownership of it. On error the receiver is unchanged. For an isolated block,
// use DecodeCommitCBOR to also enforce absence of trailing data.
func (c *Commit) UnmarshalCBOR(r io.Reader) error {
	cr := cbg.NewCborReader(r)
	major, count, err := cr.ReadHeader()
	if err != nil {
		return err
	}
	if major != cbg.MajMap || (count != 6 && count != 7) {
		return fmt.Errorf("Spaces commit must be a map with six fields and optional $type")
	}
	var out Commit
	fields := [...]string{"ikm", "mac", "rev", "sig", "ver", "hash", "$type"}
	for i := range count {
		field, err := cbg.ReadStringWithMax(cr, 5)
		if err != nil {
			return err
		}
		if field != fields[i] {
			return fmt.Errorf("invalid or noncanonical Spaces commit field: %q", field)
		}
		switch field {
		case "ikm":
			err = readFixedCBORBytes(cr, out.IKM[:])
		case "mac":
			err = readFixedCBORBytes(cr, out.MAC[:])
		case "hash":
			err = readFixedCBORBytes(cr, out.Hash[:])
		case "sig":
			out.Signature = make([]byte, 64)
			err = readFixedCBORBytes(cr, out.Signature)
		case "rev":
			var rev string
			rev, err = cbg.ReadStringWithMax(cr, 13)
			out.Revision = syntax.TID(rev)
		case "ver":
			var value uint64
			major, value, err = cr.ReadHeader()
			if err == nil && (major != cbg.MajUnsignedInt || value != uint64(commitVersion)) {
				err = fmt.Errorf("unsupported Spaces commit version")
			}
			out.Version = int64(value)
		case "$type":
			var value string
			value, err = cbg.ReadStringWithMax(cr, uint64(len(commitType)))
			if err == nil && value != commitType {
				err = fmt.Errorf("invalid Spaces commit $type")
			}
		}
		if err != nil {
			return err
		}
	}
	if err := out.validate(); err != nil {
		return err
	}
	*c = out
	return nil
}

func readFixedCBORBytes(r *cbg.CborReader, dst []byte) error {
	major, size, err := r.ReadHeader()
	if err != nil {
		return err
	}
	if major != cbg.MajByteString || size != uint64(len(dst)) {
		return fmt.Errorf("Spaces commit field must be a %d-byte CBOR byte string", len(dst))
	}
	_, err = io.ReadFull(r, dst)
	return err
}

func writeCBORString(w *cbg.CborWriter, value string) error {
	if err := w.WriteMajorTypeHeader(cbg.MajTextString, uint64(len(value))); err != nil {
		return err
	}
	_, err := io.WriteString(w, value)
	return err
}
