package mst

import (
	"context"
	"fmt"
	"io"

	"github.com/bluesky-social/indigo/atproto/data"

	"github.com/ipfs/go-cid"
	"github.com/ipfs/go-datastore"
	blockstore "github.com/ipfs/go-ipfs-blockstore"
	"github.com/ipld/go-car"
)

func LoadTreeFromCAR(ctx context.Context, r io.Reader) (*Tree, *cid.Cid, error) {

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

	obj, err := data.UnmarshalCBOR(commitBlock.RawData())
	if err != nil {
		return nil, nil, fmt.Errorf("parsing commit block from CAR file: %w", err)
	}
	raw, ok := obj["data"]
	if !ok {
		return nil, nil, fmt.Errorf("no data CID in commit block")
	}
	cl, ok := raw.(data.CIDLink)
	if !ok {
		return nil, nil, fmt.Errorf("wrong data type in commit block: %T", raw)
	}
	rootCID := cl.CID()

	n, err := loadNodeFromStore(ctx, bs, rootCID)
	if err != nil {
		return nil, nil, fmt.Errorf("reading MST from CAR file: %w", err)
	}
	nodeEnsureHeights(n)
	tree := Tree{
		Root: n,
	}
	return &tree, &rootCID, nil
}
