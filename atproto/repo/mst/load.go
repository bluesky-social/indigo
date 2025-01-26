package mst

import (
	"bytes"
	"context"

	"github.com/ipfs/go-cid"
	blockstore "github.com/ipfs/go-ipfs-blockstore"
	ipld "github.com/ipfs/go-ipld-format"
)

func loadNodeFromStore(ctx context.Context, bs blockstore.Blockstore, ref cid.Cid) (*Node, error) {
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
			child, err := loadNodeFromStore(ctx, bs, *e.ChildCID)
			if err != nil && ipld.IsNotFound(err) {
				// allow "partial" trees
				continue
			}
			if err != nil {
				return nil, err
			}
			n.Entries[i].Child = child
			// NOTE: this is kind of a hack
			if n.Height == -1 && child.Height >= 0 {
				n.Height = child.Height + 1
			}
		}
	}

	return &n, nil
}
