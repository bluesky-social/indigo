package repo

import (
	"context"
	"errors"

	"github.com/bluesky-social/indigo/atproto/repo/mst"
	"github.com/bluesky-social/indigo/atproto/syntax"

	"github.com/ipfs/go-cid"
	"github.com/ipfs/go-datastore"
	blockstore "github.com/ipfs/go-ipfs-blockstore"
)

// Version of the repo data format implemented in this package
const ATPROTO_REPO_VERSION int64 = 3

// High-level wrapper struct for an atproto repository.
type Repo struct {
	DID   syntax.DID
	Clock *syntax.TIDClock

	RecordStore blockstore.Blockstore
	MST         mst.Tree
}

var ErrNotFound = errors.New("record not found in repository")

func NewEmptyRepo(did syntax.DID) Repo {
	clk := syntax.NewTIDClock(0)
	return Repo{
		DID:         did,
		Clock:       &clk,
		RecordStore: blockstore.NewBlockstore(datastore.NewMapDatastore()),
		MST:         mst.NewEmptyTree(),
	}
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

// Snapshots the current state of the repository, resulting in a new (unsigned) `Commit` struct.
func (repo *Repo) Commit() (*Commit, error) {
	root, err := repo.MST.RootCID()
	if err != nil {
		return nil, err
	}
	c := Commit{
		DID:     repo.DID.String(),
		Version: ATPROTO_REPO_VERSION,
		Prev:    nil,
		Data:    *root,
		Rev:     repo.Clock.Next().String(),
	}
	return &c, nil
}
