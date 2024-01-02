package data

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"

	"github.com/ipfs/go-cid"
	cbg "github.com/whyrusleeping/cbor-gen"
)

// Represents the "blob" type from the atproto data model.
//
// This struct does not get marshaled/unmarshaled directly in to JSON or CBOR; see the BlobSchema and LegacyBlobSchema structs. This is the type that should be included in golang struct definitions.
//
// When representing a "legacy" blob (no size field, string CID), size == -1.
type Blob struct {
	Ref      CIDLink
	MimeType string
	Size     int64
}

type LegacyBlobSchema struct {
	Cid      string `json:"cid" cborgen:"cid"`
	MimeType string `json:"mimeType" cborgen:"mimeType"`
}

type BlobSchema struct {
	LexiconTypeID string  `json:"$type,const=blob" cborgen:"$type,const=blob"`
	Ref           CIDLink `json:"ref" cborgen:"ref"`
	MimeType      string  `json:"mimeType" cborgen:"mimeType"`
	Size          int64   `json:"size" cborgen:"size"`
}

func (b Blob) MarshalJSON() ([]byte, error) {
	if b.Size < 0 {
		lb := LegacyBlobSchema{
			Cid:      b.Ref.String(),
			MimeType: b.MimeType,
		}
		return json.Marshal(lb)
	} else {
		nb := BlobSchema{
			LexiconTypeID: "blob",
			Ref:           b.Ref,
			MimeType:      b.MimeType,
			Size:          b.Size,
		}
		return json.Marshal(nb)
	}
}

func (b *Blob) UnmarshalJSON(raw []byte) error {
	typ, err := ExtractTypeJSON(raw)
	if err != nil {
		return fmt.Errorf("parsing blob type: %v", err)
	}

	if typ == "blob" {
		var bs BlobSchema
		err := json.Unmarshal(raw, &bs)
		if err != nil {
			return fmt.Errorf("parsing blob JSON: %v", err)
		}
		b.Ref = bs.Ref
		b.MimeType = bs.MimeType
		b.Size = bs.Size
		if bs.Size < 0 {
			return fmt.Errorf("parsing blob: negative size: %d", bs.Size)
		}
	} else {
		var legacy LegacyBlobSchema
		err := json.Unmarshal(raw, &legacy)
		if err != nil {
			return fmt.Errorf("parsing legacy blob: %v", err)
		}
		refCid, err := cid.Decode(legacy.Cid)
		if err != nil {
			return fmt.Errorf("parsing CID in legacy blob: %v", err)
		}
		b.Ref = CIDLink(refCid)
		b.MimeType = legacy.MimeType
		b.Size = -1
	}
	return nil
}

func (b *Blob) MarshalCBOR(w io.Writer) error {
	if b == nil {
		_, err := w.Write(cbg.CborNull)
		return err
	}
	if b.Size < 0 {
		lb := LegacyBlobSchema{
			Cid:      b.Ref.String(),
			MimeType: b.MimeType,
		}
		return lb.MarshalCBOR(w)
	} else {
		bs := BlobSchema{
			LexiconTypeID: "blob",
			Ref:           b.Ref,
			MimeType:      b.MimeType,
			Size:          b.Size,
		}
		return bs.MarshalCBOR(w)
	}
}

func (lb *Blob) UnmarshalCBOR(r io.Reader) error {
	typ, b, err := ExtractTypeCBORReader(r)
	if err != nil {
		return fmt.Errorf("parsing $blob CBOR type: %w", err)
	}

	*lb = Blob{}
	if typ == "blob" {
		var bs BlobSchema
		err := bs.UnmarshalCBOR(bytes.NewReader(b))
		if err != nil {
			return fmt.Errorf("parsing $blob CBOR: %v", err)
		}
		lb.Ref = bs.Ref
		lb.MimeType = bs.MimeType
		lb.Size = bs.Size
		if bs.Size < 0 {
			return fmt.Errorf("parsing $blob CBOR: negative size: %d", bs.Size)
		}
	} else {
		legacy := LegacyBlobSchema{}
		err := legacy.UnmarshalCBOR(bytes.NewReader(b))
		if err != nil {
			return fmt.Errorf("parsing legacy blob CBOR: %v", err)
		}
		refCid, err := cid.Decode(legacy.Cid)
		if err != nil {
			return fmt.Errorf("parsing CID in legacy blob CBOR: %v", err)
		}
		lb.Ref = CIDLink(refCid)
		lb.MimeType = legacy.MimeType
		lb.Size = -1
	}

	return nil
}
