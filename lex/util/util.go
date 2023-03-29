package util

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"

	"github.com/ipfs/go-cid"
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

type LegacyBlob struct {
	Cid      string `json:"cid" cborgen:"cid"`
	MimeType string `json:"mimeType" cborgen:"mimeType"`
}

type CidLink struct {
	Cid string `json:"$link"`
}

type NewBlob struct {
	LexiconTypeID string  `json:"$type,omitempty"`
	Ref           CidLink `json:"ref" cborgen:"ref"`
	MimeType      string  `json:"mimeType" cborgen:"mimeType"`
	Size          int64   `json:"size" cborgen:"size"`
}

type Blob struct {
	Ref      cid.Cid `json:"ref" cborgen:"ref"`
	MimeType string  `json:"mimeType" cborgen:"mimeType"`
	Size     int64   `json:"size" cborgen:"size"`
}

func (b *Blob) MarshalJSON() ([]byte, error) {
	nb := NewBlob{
		LexiconTypeID: "blob",
		Ref:           CidLink{b.Ref.String()},
		MimeType:      b.MimeType,
		Size:          b.Size,
	}
	return json.Marshal(nb)
}

func (b *Blob) UnmarshalJSON(raw []byte) error {
	typ, err := TypeExtract(raw)
	if err != nil {
		return fmt.Errorf("parsing blob: %v", err)
	}

	if typ == "blob" {
		var nb NewBlob
		err := json.Unmarshal(raw, &nb)
		if err != nil {
			return fmt.Errorf("parsing blob JSON: %v", err)
		}
		b.Ref, err = cid.Decode(nb.Ref.Cid)
		if err != nil {
			return fmt.Errorf("parsing blob CID: %v", err)
		}
		b.MimeType = nb.MimeType
		b.Size = nb.Size
	} else {
		var legacy *LegacyBlob
		err := json.Unmarshal(raw, legacy)
		if err != nil {
			return fmt.Errorf("parsing legacy blob: %v", err)
		}
		b.Ref, err = cid.Decode(legacy.Cid)
		if err != nil {
			return fmt.Errorf("parsing CID in legacy blob: %v", err)
		}
		b.MimeType = legacy.MimeType
		// TODO: copying the -1 here from atproto behavior. should verify if it
		// should be *size instead
		b.Size = -1
	}
	return nil
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
