package repo

import (
	"context"
	"errors"
	"fmt"

	"github.com/bluesky-social/indigo/atproto/repo/mst"
	"github.com/bluesky-social/indigo/atproto/syntax"

	"github.com/ipfs/go-cid"
	"github.com/ipfs/go-datastore"
	blockstore "github.com/ipfs/go-ipfs-blockstore"
)

// Version of the repo data format implemented in this package
const ATPROTO_REPO_VERSION int64 = 3

type Commit struct {
	DID     string   `json:"did" cborgen:"did"`
	Version int64    `json:"version" cborgen:"version"` // currently: 3
	Prev    *cid.Cid `json:"prev" cborgen:"prev"`       // TODO: could we omitempty yet? breaks signatures I guess
	Data    cid.Cid  `json:"data" cborgen:"data"`
	Sig     []byte   `json:"sig" cborgen:"sig"`
	Rev     string   `json:"rev" cborgen:"rev"`
}

type Repo struct {
	DID    syntax.DID
	Clock  syntax.TIDClock
	Commit *Commit

	RecordStore blockstore.Blockstore
	MST         mst.Tree
}

var ErrNotFound = errors.New("record not found in repository")

func NewRepo(did syntax.DID) Repo {
	// TODO: why does this return a pointer?
	clk := syntax.NewTIDClock(0)
	return Repo{
		DID:         did,
		Clock:       *clk,
		Commit:      nil,
		RecordStore: blockstore.NewBlockstore(datastore.NewMapDatastore()),
		MST:         mst.NewEmptyTree(),
	}
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

func (repo *Repo) GetRecordCID(ctx context.Context, collection syntax.NSID, rkey syntax.RecordKey) (*cid.Cid, error) {
	path := collection.String() + "/" + rkey.String()
	c, err := repo.MST.Get([]byte(path))
	if err != nil {
		return nil, err
	}
	if c == nil {
		return nil, ErrNotFound
	}
	return c, nil
}

func (repo *Repo) GetRecordBytes(ctx context.Context, collection syntax.NSID, rkey syntax.RecordKey) ([]byte, error) {
	c, err := repo.GetRecordCID(ctx, collection, rkey)
	if err != nil {
		return nil, err
	}
	blk, err := repo.RecordStore.Get(ctx, *c)
	if err != nil {
		return nil, err
	}
	// TODO: not verifying CID
	return blk.RawData(), nil
}

// TODO:
// IsComplete()
// LoadFromStore
// LoadFromCAR(reader)
// WriteBlocks
// WriteCAR
// VerifyCIDs(bool)
// Export
// GetRecordStruct
// GetRecordProof
