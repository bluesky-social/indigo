package main

import (
	cbg "github.com/whyrusleeping/cbor-gen"
	"github.com/whyrusleeping/gosky/api"
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

	if err := cbg.WriteMapEncodersToFile("api/cbor_gen.go", "api", api.PostRecord{}, api.PostEntity{}, api.PostRef{}, api.ReplyRef{}); err != nil {
		panic(err)
	}
}
