package mst

import (
	"context"
	"crypto/rand"
	"fmt"
	"testing"

	cid "github.com/ipfs/go-cid"
	"github.com/ipfs/go-datastore"
	blockstore "github.com/ipfs/go-ipfs-blockstore"
	cbor "github.com/ipfs/go-ipld-cbor"
	mh "github.com/multiformats/go-multihash"
)

func randCid() cid.Cid {
	buf := make([]byte, 32)
	rand.Read(buf)
	c, err := cid.NewPrefixV1(cid.Raw, mh.SHA2_256).Sum(buf)
	if err != nil {
		panic(err)
	}
	return c
}

func TestBasicMst(t *testing.T) {
	ctx := context.Background()
	cst := cbor.NewCborStore(blockstore.NewBlockstore(datastore.NewMapDatastore()))
	mst := NewMST(cst, 16, cid.Undef, []NodeEntry{}, 0)

	vals := map[string]cid.Cid{
		"cats":           randCid(),
		"dogs":           randCid(),
		"cats and bears": randCid(),
	}

	for k, v := range vals {
		nmst, err := mst.Add(ctx, k, v, -1)
		if err != nil {
			t.Fatal(err)
		}
		mst = nmst
	}

	ncid, err := mst.getPointer(ctx)
	if err != nil {
		t.Fatal(err)
	}

	fmt.Println(ncid)
}
