package label

import (
	"bytes"
	"fmt"

	comatproto "github.com/bluesky-social/indigo/api/atproto"
	"github.com/bluesky-social/indigo/atproto/crypto"
	"github.com/bluesky-social/indigo/atproto/data"
	"github.com/bluesky-social/indigo/atproto/syntax"
)

// version of the label data fromat implemented by this package
const ATPROTO_LABEL_VERSION int64 = 1

type Label struct {
	CID       *string    `json:"cid,omitempty" cborgen:"cid,omitempty"`
	CreatedAt string     `json:"cts" cborgen:"cts"`
	ExpiresAt *string    `json:"exp,omitempty" cborgen:"exp,omitempty"`
	Negated   *bool      `json:"neg,omitempty" cborgen:"neg,omitempty"`
	SourceDID string     `json:"src" cborgen:"src"`
	URI       string     `json:"uri" cborgen:"uri"`
	Val       string     `json:"val" cborgen:"val"`
	Version   int64      `json:"ver" cborgen:"ver"`
	Sig       data.Bytes `json:"sig,omitempty" cborgen:"sig,omitempty"`
}

// converts to map[string]any for printing as JSON
func (l *Label) Data() map[string]any {
	d := map[string]any{
		"cid": l.CID,
		"cts": l.CreatedAt,
		"src": l.SourceDID,
		"uri": l.URI,
		"val": l.Val,
		"ver": l.Version,
	}
	if l.CID != nil {
		d["cid"] = l.CID
	}
	if l.ExpiresAt != nil {
		d["exp"] = l.ExpiresAt
	}
	if l.Negated != nil {
		d["neg"] = l.Negated
	}
	if l.Sig != nil {
		d["sig"] = data.Bytes(l.Sig)
	}
	return d
}

// does basic checks on syntax and structure
func (l *Label) VerifySyntax() error {
	if l.Version != ATPROTO_LABEL_VERSION {
		return fmt.Errorf("unsupported label version: %d", l.Version)
	}
	if len(l.Val) == 0 {
		return fmt.Errorf("empty label value")
	}
	if l.CID != nil {
		_, err := syntax.ParseCID(*l.CID)
		if err != nil {
			return fmt.Errorf("invalid label: %w", err)
		}
	}
	_, err := syntax.ParseDatetime(l.CreatedAt)
	if err != nil {
		return fmt.Errorf("invalid label: %w", err)
	}
	if l.ExpiresAt != nil {
		_, err := syntax.ParseDatetime(*l.ExpiresAt)
		if err != nil {
			return fmt.Errorf("invalid label: %w", err)
		}
	}
	_, err = syntax.ParseDID(l.SourceDID)
	if err != nil {
		return fmt.Errorf("invalid label: %w", err)
	}
	_, err = syntax.ParseURI(l.URI)
	if err != nil {
		return fmt.Errorf("invalid label: %w", err)
	}
	return nil
}

func (l *Label) UnsignedBytes() ([]byte, error) {
	buf := new(bytes.Buffer)
	if l.Sig == nil {
		if err := l.MarshalCBOR(buf); err != nil {
			return nil, err
		}
		return buf.Bytes(), nil
	}
	unsigned := Label{
		CID:       l.CID,
		CreatedAt: l.CreatedAt,
		ExpiresAt: l.ExpiresAt,
		Negated:   l.Negated,
		SourceDID: l.SourceDID,
		URI:       l.URI,
		Val:       l.Val,
		Version:   l.Version,
	}
	if err := unsigned.MarshalCBOR(buf); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// Signs the commit, storing the signature in the `Sig` field
func (l *Label) Sign(privkey crypto.PrivateKey) error {
	b, err := l.UnsignedBytes()
	if err != nil {
		return err
	}
	sig, err := privkey.HashAndSign(b)
	if err != nil {
		return err
	}
	l.Sig = sig
	return nil
}

// Verifies `Sig` field using the provided key. Returns `nil` if signature is valid.
func (l *Label) VerifySignature(pubkey crypto.PublicKey) error {
	if l.Sig == nil {
		return fmt.Errorf("can not verify unsigned commit")
	}
	b, err := l.UnsignedBytes()
	if err != nil {
		return err
	}
	return pubkey.HashAndVerify(b, l.Sig)
}

func (l *Label) ToLexicon() comatproto.LabelDefs_Label {
	return comatproto.LabelDefs_Label{
		Cid: l.CID,
		Cts: l.CreatedAt,
		Exp: l.ExpiresAt,
		Sig: []byte(l.Sig),
		Src: l.SourceDID,
		Uri: l.URI,
		Val: l.Val,
		Ver: &l.Version,
	}
}

func FromLexicon(l *comatproto.LabelDefs_Label) Label {
	var v int64 = 0
	if l.Ver != nil {
		v = *l.Ver
	}
	return Label{
		CID:       l.Cid,
		CreatedAt: l.Cts,
		ExpiresAt: l.Exp,
		Sig:       []byte(l.Sig),
		SourceDID: l.Src,
		URI:       l.Uri,
		Val:       l.Val,
		Version:   v,
	}
}
