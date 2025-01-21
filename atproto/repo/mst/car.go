package mst

import (
	"bytes"
	"context"
	"fmt"
	"io"

	"github.com/bluesky-social/indigo/atproto/data"

	"github.com/ipfs/go-cid"
	"github.com/ipfs/go-datastore"
	blockstore "github.com/ipfs/go-ipfs-blockstore"
	ipld "github.com/ipfs/go-ipld-format"
	"github.com/ipld/go-car/v2"
)

func HydrateNode(ctx context.Context, bs blockstore.Blockstore, ref cid.Cid) (*Node, error) {
	block, err := bs.Get(ctx, ref)
	if err != nil {
		return nil, err
	}

	nd, err := NodeDataFromCBOR(bytes.NewReader(block.RawData()))
	if err != nil {
		return nil, err
	}

	n := nd.Node(&ref)

	for i, e := range n.Entries {
		if e.IsChild() {
			child, err := HydrateNode(ctx, bs, *e.ChildCID)
			if err != nil && ipld.IsNotFound(err) {
				// allow "partial" trees
				continue
			}
			if err != nil {
				return nil, err
			}
			n.Entries[i].Child = child
		}
	}

	return &n, nil
}

func ReadTreeFromCar(ctx context.Context, r io.Reader) (*Node, *cid.Cid, error) {

	bs := blockstore.NewBlockstore(datastore.NewMapDatastore())

	br, err := car.NewBlockReader(r)
	if err != nil {
		return nil, nil, err
	}

	for {
		blk, err := br.Next()
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

	if len(br.Roots) < 1 {
		return nil, nil, fmt.Errorf("CAR file missing root CID")
	}
	commitCID := br.Roots[0]

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

	n, err := HydrateNode(ctx, bs, rootCID)
	if err != nil {
		return nil, nil, fmt.Errorf("reading MST from CAR file: %w", err)
	}
	return n, &rootCID, nil
}
