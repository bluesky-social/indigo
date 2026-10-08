package spaces

import (
	"crypto/hkdf"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/binary"
	"fmt"

	"github.com/bluesky-social/indigo/atproto/atcrypto"
	"github.com/bluesky-social/indigo/atproto/syntax"
)

const commitVersion int64 = 1
const commitProtocolTag = "atproto-space-v1"
const commitType = "com.atproto.space.defs#signedCommit"

// Commit authenticates a Spaces repo digest in the context of a space, author,
// and revision. Its signature covers the context and nonce, not Hash directly;
// MAC binds Hash to that context. This construction deliberately does not provide
// a rebroadcastable proof of the author's records.
type Commit struct {
	Version   int64
	Hash      [32]byte
	IKM       [32]byte
	MAC       [32]byte
	Signature []byte
	Revision  syntax.TID
}

// NewCommit constructs an unsigned commit with a fresh, cryptographically random
// nonce and a MAC. Call Sign before encoding or serving it. Each reader should
// receive a fresh nonce, rather than a reused signed commit.
func NewCommit(hash [32]byte, revision syntax.TID, space syntax.SpaceRef, author syntax.DID) (*Commit, error) {
	c := &Commit{Version: commitVersion, Hash: hash, Revision: revision}
	if _, err := rand.Read(c.IKM[:]); err != nil {
		return nil, err
	}
	context, err := commitContext(space, author, revision, c.IKM)
	if err != nil {
		return nil, err
	}
	c.MAC, err = commitMAC(c.IKM, context, c.Hash)
	if err != nil {
		return nil, err
	}
	return c, nil
}

// Sign signs this commit's context and recomputes its MAC with the author's
// atproto signing key. The commit nonce is retained; use NewCommit for a fresh
// reader-specific commit. Signing does not change the repo digest or revision.
func (c *Commit) Sign(space syntax.SpaceRef, author syntax.DID, key atcrypto.PrivateKey) error {
	if c == nil || c.Version != commitVersion {
		return fmt.Errorf("unsupported Spaces commit version")
	}
	if key == nil {
		return fmt.Errorf("missing Spaces commit signing key")
	}
	context, err := commitContext(space, author, c.Revision, c.IKM)
	if err != nil {
		return err
	}
	mac, err := commitMAC(c.IKM, context, c.Hash)
	if err != nil {
		return err
	}
	sig, err := key.HashAndSign(context)
	if err != nil {
		return err
	}
	if len(sig) != 64 {
		return fmt.Errorf("Spaces commit signature must be 64 bytes")
	}
	c.MAC, c.Signature = mac, sig
	return nil
}

// Verify checks the structure, author signature, and MAC against the expected
// space and author. The caller supplies the author's trusted signing key; this
// method does not resolve identity or establish how that key was obtained.
func (c *Commit) Verify(space syntax.SpaceRef, author syntax.DID, key atcrypto.PublicKey) error {
	if err := c.validate(); err != nil {
		return err
	}
	if key == nil {
		return fmt.Errorf("missing Spaces commit verification key")
	}
	context, err := commitContext(space, author, c.Revision, c.IKM)
	if err != nil {
		return err
	}
	if err := key.HashAndVerify(context, c.Signature); err != nil {
		return fmt.Errorf("verifying Spaces commit signature: %w", err)
	}
	mac, err := commitMAC(c.IKM, context, c.Hash)
	if err != nil {
		return err
	}
	if !hmac.Equal(c.MAC[:], mac[:]) {
		return fmt.Errorf("Spaces commit MAC mismatch")
	}
	return nil
}

func (c *Commit) validate() error {
	if c == nil {
		return fmt.Errorf("missing Spaces commit")
	}
	if c.Version != commitVersion {
		return fmt.Errorf("unsupported Spaces commit version: %d", c.Version)
	}
	if _, err := syntax.ParseTID(c.Revision.String()); err != nil {
		return fmt.Errorf("invalid Spaces commit revision: %w", err)
	}
	if len(c.Signature) != 64 {
		return fmt.Errorf("Spaces commit signature must be 64 bytes")
	}
	return nil
}

// Extracted from Cobalt's atspace commit context construction. The uint16
// lengths are big-endian TLS vectors, independent of LtHash's lane byte order.
func commitContext(space syntax.SpaceRef, author syntax.DID, revision syntax.TID, ikm [32]byte) ([]byte, error) {
	if _, err := syntax.ParseSpaceRef(space.String()); err != nil {
		return nil, fmt.Errorf("invalid Spaces commit space: %w", err)
	}
	if _, err := syntax.ParseDID(author.String()); err != nil {
		return nil, fmt.Errorf("invalid Spaces commit author: %w", err)
	}
	if _, err := syntax.ParseTID(revision.String()); err != nil {
		return nil, fmt.Errorf("invalid Spaces commit revision: %w", err)
	}
	if len(space) > 65535 || len(author) > 65535 || len(revision) > 65535 {
		return nil, fmt.Errorf("Spaces commit context field exceeds uint16 length")
	}
	out := make([]byte, 0, len(commitProtocolTag)+len(space)+len(author)+len(revision)+32+8)
	out = append(out, commitProtocolTag...)
	out = binary.BigEndian.AppendUint16(out, uint16(len(space)))
	out = append(out, space...)
	out = binary.BigEndian.AppendUint16(out, uint16(len(author)))
	out = append(out, author...)
	out = binary.BigEndian.AppendUint16(out, uint16(len(revision)))
	out = append(out, revision...)
	out = binary.BigEndian.AppendUint16(out, 32)
	out = append(out, ikm[:]...)
	return out, nil
}

func commitMAC(ikm [32]byte, context []byte, hash [32]byte) ([32]byte, error) {
	var out [32]byte
	key, err := hkdf.Expand(sha256.New, ikm[:], string(context), 32)
	if err != nil {
		return out, fmt.Errorf("expanding Spaces commit MAC key: %w", err)
	}
	mac := hmac.New(sha256.New, key)
	_, _ = mac.Write(hash[:])
	mac.Sum(out[:0])
	return out, nil
}
