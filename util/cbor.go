package util

import (
	"github.com/ipfs/boxo/blockstore"
	cbor "github.com/ipfs/go-ipld-cbor"
	mh "github.com/multiformats/go-multihash"
)

func CborStore(bs blockstore.Blockstore) *cbor.BasicIpldStore {
	cst := cbor.NewCborStore(bs)
	cst.DefaultMultihash = mh.SHA2_256
	return cst
}
