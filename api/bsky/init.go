package bsky

import cbg "github.com/whyrusleeping/cbor-gen"

func init() {
	cbg.MaxLength = 1000000
}
