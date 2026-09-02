package mst

import (
	"bytes"
	"context"
	"testing"

	"github.com/bluesky-social/indigo/util"

	blocks "github.com/ipfs/go-block-format"
	"github.com/ipfs/go-cid"
	"github.com/ipfs/go-datastore"
	blockstore "github.com/ipfs/go-ipfs-blockstore"
	"github.com/multiformats/go-multihash"
)

func hostileTree(t *testing.T, prefixLen int64) *MerkleSearchTree {
	val := mustCid(t, "bafkreicwamkg77pijyudfbdmskelsnuztr6gp62lqfjv3e3urbs3gxnv2m")
	nd := NodeData{
		Entries: []TreeEntry{{
			PrefixLen: prefixLen,
			KeySuffix: []byte("x"),
			Val:       val,
		}},
	}
	var buf bytes.Buffer
	if err := nd.MarshalCBOR(&buf); err != nil {
		t.Fatal(err)
	}
	c, err := cid.NewPrefixV1(cid.DagCBOR, multihash.SHA2_256).Sum(buf.Bytes())
	if err != nil {
		t.Fatal(err)
	}
	blk, err := blocks.NewBlockWithCid(buf.Bytes(), c)
	if err != nil {
		t.Fatal(err)
	}
	bs := blockstore.NewBlockstore(datastore.NewMapDatastore())
	if err := bs.Put(context.Background(), blk); err != nil {
		t.Fatal(err)
	}
	return LoadMST(util.CborStore(bs), c)
}

// Out-of-range PrefixLen on hydration must error, not panic.
func TestDeserializePrefixLenOutOfRange(t *testing.T) {
	tree := hostileTree(t, 100)
	if _, err := tree.Get(context.Background(), "x"); err == nil {
		t.Fatal("expected error for out-of-range PrefixLen")
	}
}

// Negative PrefixLen on hydration must error, not panic.
func TestDeserializePrefixLenNegative(t *testing.T) {
	tree := hostileTree(t, -1)
	if _, err := tree.Get(context.Background(), "x"); err == nil {
		t.Fatal("expected error for negative PrefixLen")
	}
}

func TestDeserializePrefixLenHuge(t *testing.T) {
	tree := hostileTree(t, 1<<40)
	if _, err := tree.Get(context.Background(), "x"); err == nil {
		t.Fatal("expected error for huge PrefixLen")
	}
}
