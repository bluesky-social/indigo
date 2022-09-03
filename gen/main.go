package main

import (
	cbg "github.com/whyrusleeping/cbor-gen"
	mst "github.com/whyrusleeping/gosky"
)

func main() {
	if err := cbg.WriteMapEncodersToFile("cbor_gen.go", "mst", mst.NodeData{}, mst.TreeEntry{}); err != nil {
		panic(err)
	}
}
