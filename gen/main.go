package main

import (
	"reflect"

	atproto "github.com/gander-social/gander-indigo-sovereign/api/atproto"
	chat "github.com/gander-social/gander-indigo-sovereign/api/chat"
	gndr "github.com/gander-social/gander-indigo-sovereign/api/gndr"
	"github.com/gander-social/gander-indigo-sovereign/atproto/data"
	"github.com/gander-social/gander-indigo-sovereign/atproto/label"
	atrepo "github.com/gander-social/gander-indigo-sovereign/atproto/repo"
	atmst "github.com/gander-social/gander-indigo-sovereign/atproto/repo/mst"
	"github.com/gander-social/gander-indigo-sovereign/events"
	lexutil "github.com/gander-social/gander-indigo-sovereign/lex/util"
	"github.com/gander-social/gander-indigo-sovereign/mst"
	"github.com/gander-social/gander-indigo-sovereign/plc"
	"github.com/gander-social/gander-indigo-sovereign/repo"
	"github.com/gander-social/gander-indigo-sovereign/util/labels"

	cbg "github.com/whyrusleeping/cbor-gen"
)

func main() {
	var typVals []any
	for _, typ := range mst.CBORTypes() {
		typVals = append(typVals, reflect.New(typ).Elem().Interface())
	}

	genCfg := cbg.Gen{
		MaxStringLength: 1_000_000,
	}

	if err := genCfg.WriteMapEncodersToFile("mst/cbor_gen.go", "mst", typVals...); err != nil {
		panic(err)
	}

	if err := genCfg.WriteMapEncodersToFile("repo/cbor_gen.go", "repo", repo.SignedCommit{}, repo.UnsignedCommit{}); err != nil {
		panic(err)
	}

	if err := genCfg.WriteMapEncodersToFile("plc/cbor_gen.go", "plc", plc.CreateOp{}); err != nil {
		panic(err)
	}

	if err := genCfg.WriteMapEncodersToFile("util/labels/cbor_gen.go", "labels", labels.UnsignedLabel{}); err != nil {
		panic(err)
	}

	if err := genCfg.WriteMapEncodersToFile("api/gndr/cbor_gen.go", "gndr",
		gndr.FeedPost{}, gndr.FeedRepost{}, gndr.FeedPost_Entity{},
		gndr.FeedPost_ReplyRef{}, gndr.FeedPost_TextSlice{}, gndr.EmbedImages{},
		gndr.EmbedExternal{}, gndr.EmbedExternal_External{},
		gndr.EmbedImages_Image{}, gndr.GraphFollow{}, gndr.ActorProfile{},
		gndr.EmbedRecord{}, gndr.FeedLike{}, gndr.RichtextFacet{},
		gndr.RichtextFacet_ByteSlice{},
		gndr.RichtextFacet_Link{}, gndr.RichtextFacet_Mention{}, gndr.RichtextFacet_Tag{},
		gndr.EmbedRecordWithMedia{},
		gndr.FeedDefs_NotFoundPost{},
		gndr.GraphBlock{},
		gndr.GraphList{},
		gndr.GraphListitem{},
		gndr.FeedGenerator{},
		gndr.GraphListblock{},
		gndr.EmbedDefs_AspectRatio{},
		gndr.FeedThreadgate{},
		gndr.FeedThreadgate_ListRule{},
		gndr.FeedThreadgate_MentionRule{},
		gndr.FeedThreadgate_FollowerRule{},
		gndr.FeedThreadgate_FollowingRule{},
		gndr.GraphStarterpack_FeedItem{},
		gndr.GraphStarterpack{},
		gndr.LabelerService{},
		gndr.LabelerDefs_LabelerPolicies{},
		gndr.EmbedVideo{}, gndr.EmbedVideo_Caption{},
		gndr.FeedPostgate{},
		gndr.FeedPostgate_DisableRule{},
		gndr.GraphVerification{},
		gndr.ActorStatus{},
		bsky.NotificationDeclaration{},
		/*gndr.EmbedImages_View{},
		gndr.EmbedRecord_View{}, gndr.EmbedRecordWithMedia_View{},
		gndr.EmbedExternal_View{}, gndr.EmbedImages_ViewImage{},
		gndr.EmbedExternal_ViewExternal{}, gndr.EmbedRecord_ViewNotFound{},
		gndr.FeedDefs_ThreadViewPost{}, gndr.EmbedRecord_ViewRecord{},
		gndr.FeedDefs_PostView{}, gndr.ActorDefs_ProfileViewBasic{},
		*/
	); err != nil {
		panic(err)
	}

	if err := genCfg.WriteMapEncodersToFile("api/chat/cbor_gen.go", "chat",
		chat.ActorDeclaration{},
	); err != nil {
		panic(err)
	}

	if err := genCfg.WriteMapEncodersToFile("api/atproto/cbor_gen.go", "atproto",
		atproto.LexiconSchema{},
		atproto.RepoStrongRef{},
		atproto.SyncSubscribeRepos_Commit{},
		atproto.SyncSubscribeRepos_Sync{},
		atproto.SyncSubscribeRepos_Identity{},
		atproto.SyncSubscribeRepos_Account{},
		atproto.SyncSubscribeRepos_Info{},
		atproto.SyncSubscribeRepos_RepoOp{},
		atproto.LabelDefs_SelfLabels{},
		atproto.LabelDefs_SelfLabel{},
		atproto.LabelDefs_Label{},
		atproto.LabelSubscribeLabels_Labels{},
		atproto.LabelSubscribeLabels_Info{},
		atproto.LabelDefs_LabelValueDefinition{},
		atproto.LabelDefs_LabelValueDefinitionStrings{},
	); err != nil {
		panic(err)
	}

	if err := genCfg.WriteMapEncodersToFile("lex/util/cbor_gen.go", "util", lexutil.CborChecker{}, lexutil.LegacyBlob{}, lexutil.BlobSchema{}); err != nil {
		panic(err)
	}

	if err := genCfg.WriteMapEncodersToFile("events/cbor_gen.go", "events", events.EventHeader{}, events.ErrorFrame{}); err != nil {
		panic(err)
	}

	if err := genCfg.WriteMapEncodersToFile("atproto/data/cbor_gen.go", "data", data.GenericRecord{}, data.LegacyBlobSchema{}, data.BlobSchema{}); err != nil {
		panic(err)
	}

	if err := genCfg.WriteMapEncodersToFile("atproto/repo/cbor_gen.go", "repo", atrepo.Commit{}); err != nil {
		panic(err)
	}

	if err := genCfg.WriteMapEncodersToFile("atproto/repo/mst/cbor_gen.go", "mst", atmst.NodeData{}, atmst.EntryData{}); err != nil {
		panic(err)
	}

	if err := genCfg.WriteMapEncodersToFile("atproto/label/cbor_gen.go", "label", label.Label{}); err != nil {
		panic(err)
	}
}
