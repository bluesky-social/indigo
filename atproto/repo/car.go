package repo

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"

	"github.com/bluesky-social/indigo/atproto/repo/mst"
	"github.com/bluesky-social/indigo/atproto/syntax"

	blocks "github.com/ipfs/go-block-format"
	"github.com/ipld/go-car"
)

func LoadFromCAR(ctx context.Context, r io.Reader) (*Commit, *Repo, error) {

	//bs := blockstore.NewBlockstore(datastore.NewMapDatastore())
	bs := NewTinyBlockstore()

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

var ErrNoRoot = errors.New("CAR file missing root CID")
var ErrNoCommit = errors.New("no commit")

// LoadCARCommit is like LoadFromCAR() but filters to only return the commit object.
// useful for subscribeRepos/firehose `#sync` message
func LoadCARCommit(ctx context.Context, r io.Reader) (*Commit, error) {
	cr, err := car.NewCarReader(r)
	if err != nil {
		return nil, err
	}
	if cr.Header.Version != 1 {
		return nil, fmt.Errorf("unsupported CAR file version: %d", cr.Header.Version)
	}
	if len(cr.Header.Roots) < 1 {
		return nil, ErrNoRoot
	}
	commitCID := cr.Header.Roots[0]
	var commitBlock blocks.Block
	for {
		blk, err := cr.Next()
		if err != nil {
			if err == io.EOF {
				break
			}
			return nil, err
		}

		if blk.Cid().Equals(commitCID) {
			commitBlock = blk
			break
		}
	}
	if commitBlock == nil {
		return nil, ErrNoCommit
	}
	var commit Commit
	if err := commit.UnmarshalCBOR(bytes.NewReader(commitBlock.RawData())); err != nil {
		return nil, fmt.Errorf("parsing commit block from CAR file: %w", err)
	}
	if err := commit.VerifyStructure(); err != nil {
		return nil, fmt.Errorf("parsing commit block from CAR file: %w", err)
	}
	return &commit, nil
}
