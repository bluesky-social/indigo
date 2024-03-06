package labels

import "bytes"

// UnsignedLabel is a label without the signature so we can validate it
type UnsignedLabel struct {
	// cid: Optionally, CID specifying the specific version of 'uri' resource this label applies to.
	CID *string `json:"cid,omitempty" cborgen:"cid,omitempty"`
	// cts: Timestamp when this label was created.
	CTS string `json:"cts" cborgen:"cts"`
	// neg: If true, this is a negation label, overwriting a previous label.
	Neg *bool `json:"neg,omitempty" cborgen:"neg,omitempty"`
	// src: DID of the actor who created this label.
	Src string `json:"src" cborgen:"src"`
	// uri: AT URI of the record, repository (account), or other resource that this label applies to.
	URI string `json:"uri" cborgen:"uri"`
	// val: The short string name of the value or type of this label.
	Val string `json:"val" cborgen:"val"`
}

// SignedLabel is a label with the signature so we can validate it
type SignedLabel struct {
	UnsignedLabel
	// sig: Signature of the label, using the src DID's key.
	Sig string `json:"sig" cborgen:"sig"`
}

// BytesForSigning returns bytes of the DAG-CBOR representation of object
func (ul *UnsignedLabel) BytesForSigning() ([]byte, error) {
	buf := new(bytes.Buffer)
	if err := ul.MarshalCBOR(buf); err != nil {
		return []byte{}, err
	}
	return buf.Bytes(), nil
}
