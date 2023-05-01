package util

import (
	"context"

	"github.com/ipfs/go-cid"
	blockstore "github.com/ipfs/go-ipfs-blockstore"
	cbor "github.com/ipfs/go-ipld-cbor"
	mh "github.com/multiformats/go-multihash"
)

func CborStore(bs blockstore.Blockstore) *cbor.BasicIpldStore {
	cst := cbor.NewCborStore(bs)
	cst.DefaultMultihash = mh.SHA2_256
	return cst
}

type CallbackWrapCborStore struct {
	Cst    cbor.IpldStore
	ReadCb func(cid.Cid)
}

func (cs *CallbackWrapCborStore) Get(ctx context.Context, c cid.Cid, out interface{}) error {
	cs.ReadCb(c)
	if err := cs.Cst.Get(ctx, c, out); err != nil {
		return err
	}

	return nil
}

func (cs *CallbackWrapCborStore) Put(ctx context.Context, v interface{}) (cid.Cid, error) {
	return cs.Cst.Put(ctx, v)
}
