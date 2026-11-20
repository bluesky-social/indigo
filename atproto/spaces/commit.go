package spaces

import (
	"fmt"

	"github.com/bluesky-social/indigo/atproto/syntax"
)

const commitVersion int64 = 1
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
	authenticated, err := authentication.commit()
	if err != nil {
		return err
	}
	if *authenticated != *c {
		return fmt.Errorf("Spaces commit header does not match response")
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
