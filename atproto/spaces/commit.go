package spaces

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/binary"
	"fmt"

	"github.com/bluesky-social/indigo/atproto/syntax"
)

const commitVersion int64 = 1
const commitProtocolTag = "atproto-space-repo-v1"
const commitType = "com.atproto.space.defs#commit"

// Commit describes a Spaces repo snapshot. Authentication is supplied separately
// for each response, never serialized into the commit.
type Commit struct {
	Version  int64
	Repo     syntax.DID
	Space    syntax.SpaceRef
	Revision syntax.TID
	Data     [32]byte
}

// NewCommit constructs a commit for the supplied repo digest and revision.
func NewCommit(data [32]byte, revision syntax.TID, space syntax.SpaceRef, author syntax.DID) (*Commit, error) {
	c := &Commit{Version: commitVersion, Repo: author, Space: space, Revision: revision, Data: data}
	if err := c.validate(); err != nil {
		return nil, err
	}
	return c, nil
}

// Verify checks the commit's structure, requested repo binding, and response MAC.
// The caller must establish the shared secret with the author's trusted KEM key.
func (c *Commit) Verify(space syntax.SpaceRef, author syntax.DID, authentication *MACVerification) error {
	if err := c.validate(); err != nil {
		return err
	}
	if c.Space != space || c.Repo != author {
		return fmt.Errorf("Spaces commit does not match requested space and author")
	}
	if authentication == nil {
		return fmt.Errorf("missing Spaces commit authentication")
	}
	mac := c.computeMAC(authentication.SharedSecret)
	if !hmac.Equal(authentication.MAC[:], mac[:]) {
		return fmt.Errorf("Spaces commit MAC mismatch")
	}
	return nil
}

// ComputeMAC authenticates this commit using a per-response shared secret.
// The resulting MAC belongs in the response header, not the encoded commit.
func (c *Commit) ComputeMAC(sharedSecret [32]byte) ([32]byte, error) {
	var zero [32]byte
	if err := c.validate(); err != nil {
		return zero, err
	}
	return c.computeMAC(sharedSecret), nil
}

func (c *Commit) computeMAC(sharedSecret [32]byte) [32]byte {
	var out [32]byte
	context := commitContext(c.Space, c.Repo, c.Revision)
	mac := hmac.New(sha256.New, sharedSecret[:])
	_, _ = mac.Write(context)
	_, _ = mac.Write(c.Data[:])
	mac.Sum(out[:0])
	return out
}

func (c *Commit) validate() error {
	if c == nil {
		return fmt.Errorf("missing Spaces commit")
	}
	if c.Version != commitVersion {
		return fmt.Errorf("unsupported Spaces commit version: %d", c.Version)
	}
	if _, err := syntax.ParseDID(c.Repo.String()); err != nil {
		return fmt.Errorf("invalid Spaces commit repo: %w", err)
	}
	if _, err := syntax.ParseSpaceRef(c.Space.String()); err != nil {
		return fmt.Errorf("invalid Spaces commit space: %w", err)
	}
	if _, err := syntax.ParseTID(c.Revision.String()); err != nil {
		return fmt.Errorf("invalid Spaces commit revision: %w", err)
	}
	return nil
}

// The syntax limits above keep all string lengths within uint16. Lengths are
// big-endian byte counts, independent of LtHash's little-endian lane encoding.
func commitContext(space syntax.SpaceRef, author syntax.DID, revision syntax.TID) []byte {
	out := make([]byte, 0, len(commitProtocolTag)+len(space)+len(author)+len(revision)+6)
	out = append(out, commitProtocolTag...)
	out = binary.BigEndian.AppendUint16(out, uint16(len(space)))
	out = append(out, space...)
	out = binary.BigEndian.AppendUint16(out, uint16(len(author)))
	out = append(out, author...)
	out = binary.BigEndian.AppendUint16(out, uint16(len(revision)))
	return append(out, revision...)
}
