package repo

import (
	"bytes"
	"fmt"

	"github.com/bluesky-social/indigo/atproto/crypto"
	"github.com/bluesky-social/indigo/atproto/syntax"

	"github.com/ipfs/go-cid"
)

// atproto repo commit object as a struct type. Can be used for direct CBOR or JSON serialization.
type Commit struct {
	DID     string   `json:"did" cborgen:"did"`
	Version int64    `json:"version" cborgen:"version"` // currently: 3
	Prev    *cid.Cid `json:"prev" cborgen:"prev"`       // NOTE: omitempty would break signature verification for repo v3
	Data    cid.Cid  `json:"data" cborgen:"data"`
	Sig     []byte   `json:"sig,omitempty" cborgen:"sig,omitempty"`
	Rev     string   `json:"rev,omitempty" cborgen:"rev,omitempty"`
}

// does basic checks that field values and syntax are correct
func (c *Commit) VerifyStructure() error {
	if c.Version != ATPROTO_REPO_VERSION {
		return fmt.Errorf("unsupported repo version: %d", c.Version)
	}
	if len(c.Sig) == 0 {
		return fmt.Errorf("empty commit signature")
	}
	_, err := syntax.ParseDID(c.DID)
	if err != nil {
		return fmt.Errorf("invalid commit data: %w", err)
	}
	_, err = syntax.ParseTID(c.Rev)
	if err != nil {
		return fmt.Errorf("invalid commit data: %w", err)
	}
	return nil
}

// Encodes the commit object as DAG-CBOR, without the signature field. Used for signing or validating signatures.
func (c *Commit) UnsignedBytes() ([]byte, error) {
	buf := new(bytes.Buffer)
	if c.Sig == nil {
		if err := c.MarshalCBOR(buf); err != nil {
			return nil, err
		}
		return buf.Bytes(), nil
	}
	unsigned := Commit{
		DID:     c.DID,
		Version: c.Version,
		Prev:    c.Prev,
		Data:    c.Data,
		Rev:     c.Rev,
	}
	if err := unsigned.MarshalCBOR(buf); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// Signs the commit, storing the signature in the `Sig` field
func (c *Commit) Sign(privkey crypto.PrivateKey) error {
	b, err := c.UnsignedBytes()
	if err != nil {
		return err
	}
	sig, err := privkey.HashAndSign(b)
	if err != nil {
		return err
	}
	c.Sig = sig
	return nil
}

// Verifies `Sig` field using the provided key. Returns `nil` if signature is valid.
func (c *Commit) VerifySignature(pubkey crypto.PublicKey) error {
	if c.Sig == nil {
		return fmt.Errorf("can not verify unsigned commit")
	}
	b, err := c.UnsignedBytes()
	if err != nil {
		return err
	}
	return pubkey.HashAndVerify(b, c.Sig)
}
