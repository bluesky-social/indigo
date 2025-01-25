package mst

import (
	"context"
	"fmt"

	bf "github.com/ipfs/go-block-format"
	"github.com/ipfs/go-cid"
	blockstore "github.com/ipfs/go-ipfs-blockstore"
	"github.com/multiformats/go-multihash"
)

// Similar to NodeCID, but pushes "dirty" blocks to a blockstore
// XXX: operate on a tree
func DiffNode(n *Node, bs blockstore.Blockstore) (*cid.Cid, error) {
	if n == nil {
		return nil, fmt.Errorf("nil tree") // TODO: wrap an error?
	}
	if !n.Dirty && n.CID != nil {
		return n.CID, nil
	}

	// ensure all children are computed
	for i, e := range n.Entries {
		if e.IsValue() && e.Dirty {
			// TODO: might push record block?
			e.Dirty = false
		}
		if !e.IsChild() {
			continue
		}
		if e.Child != nil && (e.Dirty || e.Child.Dirty) {
			cc, err := DiffNode(e.Child, bs)
			if err != nil {
				return nil, err
			}
			n.Entries[i].ChildCID = cc
			n.Entries[i].Dirty = false
		}
	}

	nd := n.NodeData()

	b, err := nd.CBOR()
	if err != nil {
		return nil, err
	}

	builder := cid.NewPrefixV1(cid.DagCBOR, multihash.SHA2_256)
	c, err := builder.Sum(b)
	if err != nil {
		return nil, err
	}
	n.CID = &c
	n.Dirty = false
	blk, err := bf.NewBlockWithCid(b, c)
	if err != nil {
		return nil, err
	}
	if err := bs.Put(context.TODO(), blk); err != nil {
		return nil, err
	}
	return &c, nil
}
