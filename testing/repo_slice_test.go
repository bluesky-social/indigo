package testing

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"testing"

	appbsky "github.com/bluesky-social/indigo/api/bsky"
	"github.com/bluesky-social/indigo/repo"
)

// ipfs dag import testing/repo_slice.car
// ipfs dag get bafyreiapesxwibnujg44xphqq23ekkozgcmnenj2onnx4gkgy4uipziyc4 | jq .
// ipfs dag get bafyreiapesxwibnujg44xphqq23ekkozgcmnenj2onnx4gkgy4uipziyc4 --output-codec=dag-cbor > testing/repo_record.cbor

func TestRepoSliceParse(t *testing.T) {
	ctx := context.TODO()
	fi, err := os.Open("testdata/repo_slice.car")
	if err != nil {
		t.Fatal(err)
	}

	sliceRepo, err := repo.ReadRepoFromCar(ctx, fi)
	if err != nil {
		t.Fatal(err)
	}

	_, rec, err := sliceRepo.GetRecord(ctx, "app.bsky.feed.post/3jquh3emtzo2o")
	if err != nil {
		t.Fatal(err)
	}

	post, suc := rec.(*appbsky.FeedPost)
	if !suc {
		t.Fatal("failed to deserialize post")
	}
	postJson, err := json.Marshal(post)
	if err != nil {
		t.Fatal(err)
	}
	fmt.Println(string(postJson))

	img := post.Embed.EmbedImages.Images[0]
	if img.Alt != "Sausage sandwich italian style" {
		t.Fatal("didn't get expected Alt text")
	}

	if !img.Image.Ref.Defined() {
		t.Fatal("got nil CID on image")
	}
	if img.Image.Ref.String() != "bafkreiblkobl6arfg3j7eft3akdhn2hmr2qmzfkefcgu4agnswvssg4a6a" {
		t.Fatal("didn't get expected image blob CID")
	}
	if img.Image.MimeType != "image/jpeg" {
		t.Fatal("didn't get expected image blob mimetype")
	}
}
