package main

import (
	"github.com/bluesky-social/indigo/api"
	atproto "github.com/bluesky-social/indigo/api/atproto"
	bsky "github.com/bluesky-social/indigo/api/bsky"
	"github.com/bluesky-social/indigo/events"
	lexutil "github.com/bluesky-social/indigo/lex/util"
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

	if err := cbg.WriteMapEncodersToFile("api/bsky/cbor_gen.go", "bsky", bsky.FeedPost{}, bsky.FeedRepost{}, bsky.FeedPost_Entity{}, bsky.FeedPost_ReplyRef{}, bsky.FeedPost_TextSlice{}, bsky.EmbedImages{}, bsky.EmbedExternal{}, bsky.EmbedExternal_External{}, bsky.EmbedImages_Image{}, bsky.GraphFollow{}, bsky.ActorProfile{}, bsky.EmbedRecord{}, bsky.FeedLike{}, bsky.RichtextFacet{}, bsky.RichtextFacet_ByteSlice{}, bsky.RichtextFacet_Features_Elem{}, bsky.RichtextFacet_Link{}, bsky.RichtextFacet_Mention{}, bsky.EmbedRecordWithMedia{}, bsky.EmbedRecordWithMedia_Media{}); err != nil {
		panic(err)
	}

	if err := cbg.WriteMapEncodersToFile("api/atproto/cbor_gen.go", "atproto", atproto.RepoStrongRef{}); err != nil {
		panic(err)
	}

	if err := cbg.WriteMapEncodersToFile("lex/util/cbor_gen.go", "util", lexutil.CborChecker{}, lexutil.LegacyBlob{}, lexutil.BlobSchema{}); err != nil {
		panic(err)
	}

	if err := cbg.WriteMapEncodersToFile("events/cbor_gen.go", "events", events.EventHeader{}, events.RepoAppend{}, events.RepoOp{}, events.InfoFrame{}, events.ErrorFrame{}, events.Label{}, events.LabelBatch{}); err != nil {
		panic(err)
	}
}
