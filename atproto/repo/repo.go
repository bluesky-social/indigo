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

type Repo struct {
	DID    syntax.DID
	Clock  *syntax.TIDClock
	Commit *Commit

	RecordStore blockstore.Blockstore
	MST         mst.Tree
}

var ErrNotFound = errors.New("record not found in repository")

func NewRepo(did syntax.DID) Repo {
	return Repo{
		DID:         did,
		Clock:       syntax.NewTIDClock(0),
		Commit:      nil,
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
