package repo

import (
	"bytes"
	"context"
	"fmt"
	"io"

	"github.com/bluesky-social/indigo/atproto/repo/mst"
	"github.com/bluesky-social/indigo/atproto/syntax"

	"github.com/ipfs/go-datastore"
	blockstore "github.com/ipfs/go-ipfs-blockstore"
	"github.com/ipld/go-car"
)

func LoadFromCAR(ctx context.Context, r io.Reader) (*Commit, *Repo, error) {

	bs := blockstore.NewBlockstore(datastore.NewMapDatastore())

	cr, err := car.NewCarReader(r)
	if err != nil {
		return nil, nil, err
	}

	if cr.Header.Version != 1 {
		return nil, nil, fmt.Errorf("unsupported CAR file version: %d", cr.Header.Version)
	}
	if len(cr.Header.Roots) < 1 {
		return nil, nil, fmt.Errorf("CAR file missing root CID")
	}
	commitCID := cr.Header.Roots[0]

	for {
		blk, err := cr.Next()
		if err != nil {
			if err == io.EOF {
				break
			}
			return nil, nil, err
		}

		if err := bs.Put(ctx, blk); err != nil {
			return nil, nil, err
		}
	}

	commitBlock, err := bs.Get(ctx, commitCID)
	if err != nil {
		return nil, nil, fmt.Errorf("reading commit block from CAR file: %w", err)
	}

	var commit Commit
	if err := commit.UnmarshalCBOR(bytes.NewReader(commitBlock.RawData())); err != nil {
		return nil, nil, fmt.Errorf("parsing commit block from CAR file: %w", err)
	}
	if err := commit.VerifyStructure(); err != nil {
		return nil, nil, fmt.Errorf("parsing commit block from CAR file: %w", err)
	}

	tree, err := mst.LoadTreeFromStore(ctx, bs, commit.Data)
	if err != nil {
		return nil, nil, fmt.Errorf("reading MST from CAR file: %w", err)
	}
	clk := syntax.ClockFromTID(syntax.TID(commit.Rev))
	repo := Repo{
		DID:         syntax.DID(commit.DID), // NOTE: VerifyStructure() already checked DID syntax
		Clock:       &clk,
		MST:         *tree,
		RecordStore: bs, // TODO: put just records in a smaller blockstore?
	}
	return &commit, &repo, nil
}
