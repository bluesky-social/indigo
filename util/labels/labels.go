package labels

import (
	"bytes"

	"github.com/bluesky-social/indigo/api/atproto"
)

// UnsignedLabel is a label without the signature so we can validate it
type UnsignedLabel struct {
	// cid: Optionally, CID specifying the specific version of 'uri' resource this label applies to.
	Cid *string `json:"cid,omitempty" cborgen:"cid,omitempty"`
	// cts: Timestamp when this label was created.
	Cts string `json:"cts" cborgen:"cts"`
	// exp: Timestamp at which this label expires (no longer applies).
	Exp *string `json:"exp,omitempty" cborgen:"exp,omitempty"`
	// neg: If true, this is a negation label, overwriting a previous label.
	Neg *bool `json:"neg,omitempty" cborgen:"neg,omitempty"`
	// src: DID of the actor who created this label.
	Src string `json:"src" cborgen:"src"`
	// uri: AT URI of the record, repository (account), or other resource that this label applies to.
	Uri string `json:"uri" cborgen:"uri"`
	// val: The short string name of the value or type of this label.
	Val string `json:"val" cborgen:"val"`
	// ver: The AT Protocol version of the label object.
	Ver *int64 `json:"ver,omitempty" cborgen:"ver,omitempty"`
}

// SignedLabel is a label with a signature, this type is generated via lexgen but aliased here for convenience
type SignedLabel atproto.LabelDefs_Label

// BytesForSigning returns bytes of the DAG-CBOR representation of object
func (ul *UnsignedLabel) BytesForSigning() ([]byte, error) {
	buf := new(bytes.Buffer)
	if err := ul.MarshalCBOR(buf); err != nil {
		return []byte{}, err
	}
	return buf.Bytes(), nil
}
