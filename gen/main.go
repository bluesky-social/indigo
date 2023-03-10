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

	if err := cbg.WriteMapEncodersToFile("repo/cbor_gen.go", "repo", repo.SignedCommit{}, repo.UnsignedCommit{}); err != nil {
		panic(err)
	}

	if err := cbg.WriteMapEncodersToFile("api/cbor_gen.go", "api", api.CreateOp{}); err != nil {
		panic(err)
	}

	if err := cbg.WriteMapEncodersToFile("api/bsky/cbor_gen.go", "bsky", bsky.FeedPost{}, bsky.FeedRepost{}, bsky.FeedVote{}, bsky.FeedPost_Entity{}, bsky.FeedPost_ReplyRef{}, bsky.FeedPost_TextSlice{}, bsky.EmbedImages{}, bsky.EmbedImages_PresentedImage{}, bsky.EmbedExternal{}, bsky.EmbedExternal_External{}, bsky.EmbedImages_Image{}, bsky.GraphFollow{}, bsky.ActorRef{}, bsky.ActorProfile{}, bsky.SystemDeclaration{}, bsky.GraphAssertion{}, bsky.GraphConfirmation{}, bsky.EmbedRecord{}); err != nil {
		panic(err)
	}

	if err := cbg.WriteMapEncodersToFile("api/atproto/cbor_gen.go", "atproto", atproto.RepoStrongRef{}); err != nil {
		panic(err)
	}

	if err := cbg.WriteMapEncodersToFile("lex/util/cbor_gen.go", "util", util.CborChecker{}, util.Blob{}); err != nil {
		panic(err)
	}

	if err := cbg.WriteMapEncodersToFile("events/cbor_gen.go", "events", events.EventHeader{}, events.RepoAppend{}, events.RepoOp{}, events.InfoFrame{}, events.ErrorFrame{}); err != nil {
		panic(err)
	}
}
