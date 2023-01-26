package main

import (
	"github.com/bluesky-social/indigo/api"
	atproto "github.com/bluesky-social/indigo/api/atproto"
	bsky "github.com/bluesky-social/indigo/api/bsky"
	"github.com/bluesky-social/indigo/events"
	"github.com/bluesky-social/indigo/lex/util"
	mst "github.com/bluesky-social/indigo/mst"
	"github.com/bluesky-social/indigo/repo"
	cbg "github.com/whyrusleeping/cbor-gen"
)

func main() {
	if err := cbg.WriteMapEncodersToFile("mst/cbor_gen.go", "mst", mst.NodeData{}, mst.TreeEntry{}); err != nil {
		panic(err)
	}

	if err := cbg.WriteMapEncodersToFile("repo/cbor_gen.go", "repo", repo.SignedRoot{}, repo.Meta{}, repo.Commit{}); err != nil {
		panic(err)
	}

	if err := cbg.WriteMapEncodersToFile("api/cbor_gen.go", "api", api.PostRecord{}, api.PostEntity{}, api.PostRef{}, api.ReplyRef{}, api.TextSlice{}, api.CreateOp{}); err != nil {
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

	if err := cbg.WriteMapEncodersToFile("api/bsky/cbor_gen.go", "schemagen", bsky.FeedPost{}, bsky.FeedRepost{}, bsky.FeedTrend{}, bsky.FeedVote{}, bsky.FeedPost_Entity{}, bsky.FeedPost_ReplyRef{}, bsky.FeedPost_TextSlice{}, bsky.EmbedImages{}, bsky.EmbedImages_PresentedImage{}, bsky.EmbedExternal{}, bsky.EmbedExternal_External{}, bsky.EmbedImages_Image{}, bsky.GraphFollow{}, bsky.ActorRef{}, bsky.ActorProfile{}, bsky.SystemDeclaration{}, bsky.GraphAssertion{}, bsky.GraphConfirmation{}); err != nil {
		panic(err)
	}

	if err := cbg.WriteMapEncodersToFile("api/atproto/cbor_gen.go", "schemagen", atproto.RepoStrongRef{}); err != nil {
		panic(err)
	}

	if err := cbg.WriteMapEncodersToFile("lex/util/cbor_gen.go", "util", util.CborChecker{}, util.Blob{}); err != nil {
		panic(err)
	}

	if err := cbg.WriteMapEncodersToFile("events/cbor_gen.go", "events", events.EventHeader{}, events.RepoEvent{}, events.RepoAppend{}, events.RepoOp{}); err != nil {
		panic(err)
	}
}
