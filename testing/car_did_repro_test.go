package testing

import (
	"bytes"
	"context"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"testing"

	appbsky "github.com/bluesky-social/indigo/api/bsky"
	"github.com/bluesky-social/indigo/repo"
	"github.com/bluesky-social/indigo/util"

	"github.com/ipfs/go-cid"
	"github.com/ipfs/go-datastore"
	blockstore "github.com/ipfs/go-ipfs-blockstore"
	"github.com/stretchr/testify/assert"
	cbg "github.com/whyrusleeping/cbor-gen"
	"github.com/whyrusleeping/go-did"
)

// deep verificatoin of repo: signature (against DID doc), MST structure,
// record encoding (JSON and CBOR), etc
func deepReproduceRepo(t *testing.T, carPath, docPath string) {
	ctx := context.TODO()
	assert := assert.New(t)

	// NOTE bgs/bgs.go:537 if we need to parse handle from did doc
	didDoc := mustReadDidDoc(t, docPath)
	pubkey, err := didDoc.GetPublicKey("#atproto")
	if err != nil {
		t.Fatal(err)
	}

	fi, err := os.Open(carPath)
	if err != nil {
		t.Fatal(err)
	}

	origRepo, err := repo.ReadRepoFromCar(ctx, fi)
	if err != nil {
		t.Fatal(err)
	}

	// verify signature against pubkey
	scommit := origRepo.SignedCommit()
	msg, err := scommit.Unsigned().BytesForSigning()
	if err != nil {
		t.Fatal(err)
	}
	if err := pubkey.Verify(msg, scommit.Sig); err != nil {
		fmt.Printf("didDoc: %v\n", didDoc)
		fmt.Printf("key: %v\n", pubkey)
		fmt.Printf("sig: %v\n", scommit.Sig)
		assert.NoError(err)
	}

	// enumerate all keys
	repoMap := make(map[string]cid.Cid)
	err = origRepo.ForEach(ctx, "", func(k string, v cid.Cid) error {
		repoMap[k] = v
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}

	bs := blockstore.NewBlockstore(datastore.NewMapDatastore())
	secondRepo := repo.NewRepo(ctx, didDoc.ID.String(), bs)
	for p, c := range repoMap {
		_, rec, err := origRepo.GetRecord(ctx, p)
		if err != nil {
			t.Fatal(err)
		}
		reproduceRecord(t, p, c, rec)
		secondRepo.PutRecord(ctx, p, rec)
	}

	// verify MST tree reproduced
	kmgr := &util.FakeKeyManager{}
	_, _, err = secondRepo.Commit(ctx, kmgr.SignForUser)
	if err != nil {
		t.Fatal(err)
	}
	secondCommit := secondRepo.SignedCommit()
	assert.Equal(scommit.Data.String(), secondCommit.Data.String())
}

// from JSON file on disk
func mustReadDidDoc(t *testing.T, docPath string) did.Document {
	var didDoc did.Document
	docFile, err := os.Open(docPath)
	if err != nil {
		t.Fatal(err)
	}
	if err := json.NewDecoder(docFile).Decode(&didDoc); err != nil {
		t.Fatal(err)
	}
	return didDoc
}

// deserializes and re-serializes in a couple different ways and verifies CID
func reproduceRecord(t *testing.T, path string, c cid.Cid, rec cbg.CBORMarshaler) {
	assert := assert.New(t)
	// 0x71 = dag-cbor, 0x12 = sha2-256, 0 = default length
	cidBuilder := cid.V1Builder{Codec: 0x71, MhType: 0x12, MhLength: 0}
	recordCBOR := new(bytes.Buffer)
	nsid := strings.SplitN(path, "/", 2)[0]

	// TODO: refactor this to be short+generic
	switch nsid {
	case "app.bsky.feed.post":
		var recordRepro appbsky.FeedPost
		recordOrig, suc := rec.(*appbsky.FeedPost)
		assert.Equal(true, suc)
		recordJSON, err := json.Marshal(recordOrig)
		assert.NoError(err)
		assert.NoError(json.Unmarshal(recordJSON, &recordRepro))
		assert.Equal(*recordOrig, recordRepro)
		assert.NoError(recordRepro.MarshalCBOR(recordCBOR))
		reproCID, err := cidBuilder.Sum(recordCBOR.Bytes())
		assert.NoError(err)
		if c.String() != reproCID.String() {
			fmt.Println(string(recordJSON))
			fmt.Println(hex.EncodeToString(recordCBOR.Bytes()))
			recordBytes := new(bytes.Buffer)
			assert.NoError(rec.MarshalCBOR(recordBytes))
			fmt.Println(hex.EncodeToString(recordBytes.Bytes()))
		}
		assert.Equal(c.String(), reproCID.String())
	case "app.bsky.actor.profile":
		var recordRepro appbsky.ActorProfile
		recordOrig, suc := rec.(*appbsky.ActorProfile)

		assert.Equal(true, suc)
		recordJSON, err := json.Marshal(recordOrig)
		assert.NoError(err)
		assert.NoError(json.Unmarshal(recordJSON, &recordRepro))
		assert.Equal(*recordOrig, recordRepro)
		assert.NoError(recordRepro.MarshalCBOR(recordCBOR))
		reproCID, err := cidBuilder.Sum(recordCBOR.Bytes())
		assert.NoError(err)
		assert.Equal(c.String(), reproCID.String())
	case "app.bsky.graph.follow":
		var recordRepro appbsky.GraphFollow
		recordOrig, suc := rec.(*appbsky.GraphFollow)

		assert.Equal(true, suc)
		recordJSON, err := json.Marshal(recordOrig)
		assert.NoError(err)
		assert.NoError(json.Unmarshal(recordJSON, &recordRepro))
		assert.Equal(*recordOrig, recordRepro)
		assert.NoError(recordRepro.MarshalCBOR(recordCBOR))
		reproCID, err := cidBuilder.Sum(recordCBOR.Bytes())
		assert.NoError(err)
		assert.Equal(c.String(), reproCID.String())
	case "app.bsky.feed.repost":
		var recordRepro appbsky.FeedRepost
		recordOrig, suc := rec.(*appbsky.FeedRepost)

		assert.Equal(true, suc)
		recordJSON, err := json.Marshal(recordOrig)
		assert.NoError(err)
		assert.NoError(json.Unmarshal(recordJSON, &recordRepro))
		assert.Equal(*recordOrig, recordRepro)
		assert.NoError(recordRepro.MarshalCBOR(recordCBOR))
		reproCID, err := cidBuilder.Sum(recordCBOR.Bytes())
		assert.NoError(err)
		assert.Equal(c.String(), reproCID.String())
	case "app.bsky.feed.like":
		var recordRepro appbsky.FeedLike
		recordOrig, suc := rec.(*appbsky.FeedLike)

		assert.Equal(true, suc)
		recordJSON, err := json.Marshal(recordOrig)
		assert.NoError(err)
		assert.NoError(json.Unmarshal(recordJSON, &recordRepro))
		assert.Equal(*recordOrig, recordRepro)
		assert.NoError(recordRepro.MarshalCBOR(recordCBOR))
		reproCID, err := cidBuilder.Sum(recordCBOR.Bytes())
		assert.NoError(err)
		assert.Equal(c.String(), reproCID.String())
	default:
		t.Fatal(fmt.Errorf("unsupported record type: %s", nsid))
	}
}

func TestReproduceRepo(t *testing.T) {

	// to get from prod, first resolve handle then save DID doc and repo CAR file like:
	// 	http get https://bsky.social/xrpc/com.atproto.identity.resolveHandle handle==greenground.bsky.social
	//  http get https://plc.directory/did:plc:wqgdnqlv2mwiio6pfchwtrff > greenground.didDoc.json
	//  http get https://bsky.social/xrpc/com.atproto.sync.getRepo did==did:plc:wqgdnqlv2mwiio6pfchwtrff > greenground.repo.car

	// to fetch from local dev:
	//  http get localhost:2582/did:plc:dpg45vsnuir2vqqqadsn6afg > fakermaker.didDoc.json
	//  http get localhost:2583/xrpc/com.atproto.sync.getRepo did==did:plc:dpg45vsnuir2vqqqadsn6afg > fakermaker.repo.car

	deepReproduceRepo(t, "testdata/greenground.repo.car", "testdata/greenground.didDoc.json")

	// TODO: update this with the now working p256 code
	//deepReproduceRepo(t, "testdata/fakermaker.repo.car", "testdata/fakermaker.didDoc.json")

	// XXX: currently failing
	//deepReproduceRepo(t, "testdata/paul_staging.repo.car", "testdata/paul_staging.didDoc.json")
}
