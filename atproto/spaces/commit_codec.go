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

// DecodeCommitJSON decodes a protocol commit, including atproto's unpadded
// standard-base64 $bytes representation. It validates structure, not its MAC.
func DecodeCommitJSON(data []byte) (*Commit, error) {
	var c Commit
	if err := c.UnmarshalJSON(data); err != nil {
		return nil, err
	}
	return &c, nil
}

// MarshalJSON encodes a commit with a protocol $bytes object for data.
func (c Commit) MarshalJSON() ([]byte, error) {
	if err := c.validate(); err != nil {
		return nil, err
	}
	return json.Marshal(struct {
		Version  int64           `json:"ver"`
		Repo     syntax.DID      `json:"repo"`
		Space    syntax.SpaceRef `json:"space"`
		Revision syntax.TID      `json:"rev"`
		Data     atdata.Bytes    `json:"data"`
	}{c.Version, c.Repo, c.Space, c.Revision, c.Data[:]})
}

// UnmarshalJSON decodes a commit, leaving the receiver unchanged on error.
// An optional $type must name com.atproto.space.defs#commit.
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
		case "repo":
			bit = 2
		case "space":
			bit = 4
		case "rev":
			bit = 8
		case "data":
			bit = 16
		case "$type":
			bit = 32
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
		case "data":
			var raw json.RawMessage
			if err := dec.Decode(&raw); err != nil {
				return err
			}
			// Enforce exactly one $bytes member, including duplicate rejection.
			inner := json.NewDecoder(bytes.NewReader(raw))
			if token, err := inner.Token(); err != nil || token != json.Delim('{') {
				return fmt.Errorf("Spaces commit data must be a $bytes object")
			}
			if token, err := inner.Token(); err != nil || token != "$bytes" {
				return fmt.Errorf("Spaces commit data must be a $bytes object")
			}
			var encoded string
			if err := inner.Decode(&encoded); err != nil || inner.More() {
				return fmt.Errorf("Spaces commit data must contain exactly one $bytes string")
			}
			var value atdata.Bytes
			if err := value.UnmarshalJSON(raw); err != nil {
				return err
			}
			if len(value) != len(out.Data) {
				return fmt.Errorf("Spaces commit data must be 32 bytes")
			}
			copy(out.Data[:], value)
		default:
			var value *string
			if err := dec.Decode(&value); err != nil {
				return err
			}
			if value == nil {
				return fmt.Errorf("missing Spaces commit %s", field)
			}
			switch field {
			case "repo":
				out.Repo = syntax.DID(*value)
			case "space":
				out.Space = syntax.SpaceRef(*value)
			case "rev":
				out.Revision = syntax.TID(*value)
			case "$type":
				if *value != commitType {
					return fmt.Errorf("invalid Spaces commit $type")
				}
			}
		}
	}
	if _, err := dec.Token(); err != nil {
		return err
	}
	if seen&31 != 31 {
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

// DecodeCommitCBOR decodes exactly one canonical DRISL commit, rejecting trailing
// bytes. It validates structure, not the separately supplied response MAC.
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

// MarshalCBOR writes a canonical DRISL commit with a byte string for data.
func (c *Commit) MarshalCBOR(w io.Writer) error {
	if err := c.validate(); err != nil {
		return err
	}
	cw := cbg.NewCborWriter(w)
	if err := cw.WriteMajorTypeHeader(cbg.MajMap, 5); err != nil {
		return err
	}
	// Canonical map order: shortest key first, then bytewise.
	for _, field := range [...]string{"rev", "ver", "data", "repo", "space"} {
		if err := writeCBORString(cw, field); err != nil {
			return err
		}
		var err error
		switch field {
		case "rev":
			err = writeCBORString(cw, c.Revision.String())
		case "ver":
			err = cw.WriteMajorTypeHeader(cbg.MajUnsignedInt, uint64(c.Version))
		case "data":
			err = cbg.WriteByteArray(cw, c.Data[:])
		case "repo":
			err = writeCBORString(cw, c.Repo.String())
		case "space":
			err = writeCBORString(cw, c.Space.String())
		}
		if err != nil {
			return err
		}
	}
	return nil
}

// UnmarshalCBOR reads one canonical DRISL commit without closing r. On error the
// receiver is unchanged. DecodeCommitCBOR also enforces absence of trailing data.
func (c *Commit) UnmarshalCBOR(r io.Reader) error {
	out, err := readCommitCBOR(r, commitType, false)
	if err != nil {
		return err
	}
	*c = *out
	return nil
}

// STAR uses the same fields as a standalone commit, but requires its own $type.
func readCommitCBOR(r io.Reader, expectedType string, requireType bool) (*Commit, error) {
	cr := cbg.NewCborReader(r)
	major, count, err := cr.ReadHeader()
	if err != nil {
		return nil, err
	}
	if major != cbg.MajMap || (count != 5 && count != 6) || (requireType && count != 6) {
		return nil, fmt.Errorf("Spaces commit requires five fields and the expected $type")
	}
	var out Commit
	fields := [...]string{"rev", "ver", "data", "repo", "$type", "space"}
	for i := range count {
		field, err := cbg.ReadStringWithMax(cr, 5)
		if err != nil {
			return nil, err
		}
		expected := fields[i]
		if count == 5 && i == 4 {
			expected = "space"
		}
		if field != expected {
			return nil, fmt.Errorf("invalid or noncanonical Spaces commit field: %q", field)
		}
		switch field {
		case "data":
			err = readFixedCBORBytes(cr, out.Data[:])
		case "rev":
			var value string
			value, err = cbg.ReadStringWithMax(cr, 13)
			out.Revision = syntax.TID(value)
		case "repo":
			var value string
			value, err = cbg.ReadStringWithMax(cr, 2048)
			out.Repo = syntax.DID(value)
		case "space":
			var value string
			value, err = cbg.ReadStringWithMax(cr, 8192)
			out.Space = syntax.SpaceRef(value)
		case "ver":
			var value uint64
			major, value, err = cr.ReadHeader()
			if err == nil && (major != cbg.MajUnsignedInt || value != uint64(commitVersion)) {
				err = fmt.Errorf("unsupported Spaces commit version")
			}
			out.Version = int64(value)
		case "$type":
			var value string
			value, err = cbg.ReadStringWithMax(cr, uint64(len(expectedType)))
			if err == nil && value != expectedType {
				err = fmt.Errorf("invalid Spaces commit $type")
			}
		}
		if err != nil {
			return nil, err
		}
	}
	if err := out.validate(); err != nil {
		return nil, err
	}
	return &out, nil
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
