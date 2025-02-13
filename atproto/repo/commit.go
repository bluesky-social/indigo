package repo

import (
	"fmt"

	"github.com/bluesky-social/indigo/atproto/syntax"

	"github.com/ipfs/go-cid"
)

type Commit struct {
	DID     string   `json:"did" cborgen:"did"`
	Version int64    `json:"version" cborgen:"version"` // currently: 3
	Prev    *cid.Cid `json:"prev" cborgen:"prev"`       // TODO: could we omitempty yet? breaks signatures I guess
	Data    cid.Cid  `json:"data" cborgen:"data"`
	Sig     []byte   `json:"sig" cborgen:"sig"`
	Rev     string   `json:"rev" cborgen:"rev"`
}

// does basic checks that syntax is correct
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
