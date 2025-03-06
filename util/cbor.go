package util

import (
	cbor "github.com/ipfs/go-ipld-cbor"
	mh "github.com/multiformats/go-multihash"
)

func CborStore(bs cbor.IpldBlockstore) *cbor.BasicIpldStore {
	cst := cbor.NewCborStore(bs)
	cst.DefaultMultihash = mh.SHA2_256
	return cst
}
