package main

import (
	cbg "github.com/whyrusleeping/cbor-gen"
	"github.com/whyrusleeping/gosky/api"
	atproto "github.com/whyrusleeping/gosky/api/atproto"
	bsky "github.com/whyrusleeping/gosky/api/bsky"
	"github.com/whyrusleeping/gosky/lex/util"
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

	if err := cbg.WriteMapEncodersToFile("api/cbor_gen.go", "api", api.PostRecord{}, api.PostEntity{}, api.PostRef{}, api.ReplyRef{}, api.TextSlice{}); err != nil {
		panic(err)
	}

	// RECORDTYPE: GraphFollow
	// RECORDTYPE: GraphAssertion
	// RECORDTYPE: GraphConfirmation
	// RECORDTYPE: SystemDeclaration
	// RECORDTYPE: ActorProfile
	// RECORDTYPE: FeedVote
	// RECORDTYPE: FeedTrend
	// RECORDTYPE: FeedRepost
	// RECORDTYPE: FeedPost

	if err := cbg.WriteMapEncodersToFile("api/bsky/cbor_gen.go", "schemagen", bsky.FeedPost{}, bsky.FeedRepost{}, bsky.FeedTrend{}, bsky.FeedVote{}, bsky.FeedPost_Entity{}, bsky.FeedPost_ReplyRef{}, bsky.FeedPost_TextSlice{}); err != nil {
		panic(err)
	}

	if err := cbg.WriteMapEncodersToFile("api/atproto/cbor_gen.go", "schemagen", atproto.RepoStrongRef{}); err != nil {
		panic(err)
	}

	if err := cbg.WriteMapEncodersToFile("lex/util/cbor_gen.go", "util", util.CborChecker{}); err != nil {
		panic(err)
	}
}
