package main

import (
	cbg "github.com/whyrusleeping/cbor-gen"
	mst "github.com/whyrusleeping/gosky/mst"
	"github.com/whyrusleeping/gosky/repo"
)

func main() {
	if err := cbg.WriteMapEncodersToFile("mst/cbor_gen.go", "mst", mst.NodeData{}, mst.TreeEntry{}); err != nil {
		panic(err)
	}

	if err := cbg.WriteMapEncodersToFile("repo/cbor_gen.go", "repo", repo.SignedRoot{}, repo.Meta{}, repo.Commit{}); err != nil {
		panic(err)
	}
}
