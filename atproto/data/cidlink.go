package data

import (
	"encoding/json"
	"fmt"
	"io"

	"github.com/ipfs/go-cid"
	cbg "github.com/whyrusleeping/cbor-gen"
)

// Represents the "cid-link" type from the atproto data model.
//
// Implementation is a simple wrapper around the github.com/ipfs/go-cid "cid.Cid" type.
type CIDLink cid.Cid

type jsonLink struct {
	Link string `json:"$link"`
}

// Unwraps the inner cid.Cid type (github.com/ipfs/go-cid)
func (ll CIDLink) CID() cid.Cid {
	return cid.Cid(ll)
}

// Returns string representation.
//
// If the CID is "undefined", returns an empty string (note that this is different from how cid.Cid works).
func (ll CIDLink) String() string {
	if ll.IsDefined() {
		return cid.Cid(ll).String()
	}
	return ""
}

// Convenience helper, returns false if CID is "undefined" (golang zero value)
func (ll CIDLink) IsDefined() bool {
	return cid.Cid(ll).Defined()
}

func (ll CIDLink) MarshalJSON() ([]byte, error) {
	if !ll.IsDefined() {
		return nil, fmt.Errorf("tried to marshal nil or undefined cid-link")
	}
	jl := jsonLink{
		Link: ll.String(),
	}
	return json.Marshal(jl)
}

func (ll *CIDLink) UnmarshalJSON(raw []byte) error {
	var jl jsonLink
	if err := json.Unmarshal(raw, &jl); err != nil {
		return fmt.Errorf("parsing cid-link JSON: %v", err)
	}

	c, err := cid.Decode(jl.Link)
	if err != nil {
		return fmt.Errorf("parsing cid-link CID: %v", err)
	}
	*ll = CIDLink(c)
	return nil
}

func (ll *CIDLink) MarshalCBOR(w io.Writer) error {
	if ll == nil {
		_, err := w.Write(cbg.CborNull)
		return err
	}
	if !ll.IsDefined() {
		return fmt.Errorf("tried to marshal nil or undefined cid-link")
	}
	cw := cbg.NewCborWriter(w)
	if err := cbg.WriteCid(cw, cid.Cid(*ll)); err != nil {
		return fmt.Errorf("failed to write cid-link as CBOR: %w", err)
	}
	return nil
}

func (ll *CIDLink) UnmarshalCBOR(r io.Reader) error {
	cr := cbg.NewCborReader(r)
	c, err := cbg.ReadCid(cr)
	if err != nil {
		return fmt.Errorf("failed to read cid-link from CBOR: %w", err)
	}
	*ll = CIDLink(c)
	return nil
}
