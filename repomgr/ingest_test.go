package repomgr

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	atproto "github.com/bluesky-social/indigo/api/atproto"
	bsky "github.com/bluesky-social/indigo/api/bsky"
	"github.com/bluesky-social/indigo/carstore"
	"github.com/bluesky-social/indigo/models"
	"github.com/bluesky-social/indigo/repo"
	"github.com/bluesky-social/indigo/util"
	"github.com/ipfs/go-cid"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func skipIfNoFile(t *testing.T, f string) {
	t.Helper()
	_, err := os.Stat(f)
	if err != nil {
		if os.IsNotExist(err) {
			t.Skipf("test vector %s not present, skipping for now", f)
		}

		t.Fatal(err)
	}
}

func TestLoadNewRepo(t *testing.T) {
	skipIfNoFile(t, "testrepo.car")

	dir, err := os.MkdirTemp("", "integtest")
	if err != nil {
		t.Fatal(err)
	}

	cardb, err := gorm.Open(sqlite.Open(filepath.Join(dir, "car.sqlite")))
	if err != nil {
		t.Fatal(err)
	}

	cspath := filepath.Join(dir, "carstore")
	if err := os.Mkdir(cspath, 0775); err != nil {
		t.Fatal(err)
	}

	cs, err := carstore.NewCarStore(cardb, []string{cspath})
	if err != nil {
		t.Fatal(err)
	}

	repoman := NewRepoManager(cs, &util.FakeKeyManager{})

	fi, err := os.Open("../testing/testdata/divy.repo")
	if err != nil {
		t.Fatal(err)
	}
	defer fi.Close()

	ctx := context.TODO()
	if err := repoman.ImportNewRepo(ctx, 2, "", fi, nil); err != nil {
		t.Fatal(err)
	}
}

func testCarstore(t *testing.T, dir string) carstore.CarStore {
	cardb, err := gorm.Open(sqlite.Open(filepath.Join(dir, "car.sqlite")))
	if err != nil {
		t.Fatal(err)
	}

	cspath := filepath.Join(dir, "carstore")
	if err := os.Mkdir(cspath, 0775); err != nil {
		t.Fatal(err)
	}

	/*
		cs, err := carstore.NewCarStore(cardb, []string{cspath})
		if err != nil {
			t.Fatal(err)
		}
	*/

	cs, err := carstore.NewNonArchivalCarstore(cardb)
	if err != nil {
		t.Fatal(err)
	}

	return cs
}

func TestIngestWithGap(t *testing.T) {
	dir, err := os.MkdirTemp("", "integtest")
	if err != nil {
		t.Fatal(err)
	}

	maindb, err := gorm.Open(sqlite.Open(filepath.Join(dir, "test.sqlite")))
	if err != nil {
		t.Fatal(err)
	}
	maindb.AutoMigrate(models.ActorInfo{})

	did := "did:plc:beepboop"
	maindb.Create(&models.ActorInfo{
		Did: did,
		Uid: 1,
	})

	cs := testCarstore(t, dir)

	repoman := NewRepoManager(cs, &util.FakeKeyManager{})

	dir2, err := os.MkdirTemp("", "integtest")
	if err != nil {
		t.Fatal(err)
	}
	cs2 := testCarstore(t, dir2)

	var since *string
	ctx := context.TODO()
	for i := 0; i < 5; i++ {
		slice, _, nrev, tid := doPost(t, cs2, did, since, i)

		ops := []*atproto.SyncSubscribeRepos_RepoOp{
			{
				Action: "create",
				Path:   "app.bsky.feed.post/" + tid,
			},
		}

		if err := repoman.HandleExternalUserEvent(ctx, 1, 1, did, since, nrev, slice, ops); err != nil {
			t.Fatal(err)
		}

		since = &nrev
	}

	// now do a few outside of the standard event stream flow
	for i := 0; i < 5; i++ {
		_, _, nrev, _ := doPost(t, cs2, did, since, i)
		since = &nrev
	}

	buf := new(bytes.Buffer)
	if err := cs2.ReadUserCar(ctx, 1, "", true, buf); err != nil {
		t.Fatal(err)
	}

	if err := repoman.ImportNewRepo(ctx, 1, did, buf, nil); err != nil {
		t.Fatal(err)
	}
}

func doPost(t *testing.T, cs carstore.CarStore, did string, prev *string, postid int) ([]byte, cid.Cid, string, string) {
	ctx := context.TODO()
	ds, err := cs.NewDeltaSession(ctx, 1, prev)
	if err != nil {
		t.Fatal(err)
	}

	r := repo.NewRepo(ctx, did, ds)

	_, tid, err := r.CreateRecord(ctx, "app.bsky.feed.post", &bsky.FeedPost{
		Text: fmt.Sprintf("hello friend %d", postid),
	})
	if err != nil {
		t.Fatal(err)
	}

	root, nrev, err := r.Commit(ctx, func(context.Context, string, []byte) ([]byte, error) { return nil, nil })
	if err != nil {
		t.Fatal(err)
	}

	slice, err := ds.CloseWithRoot(ctx, root, nrev)
	if err != nil {
		t.Fatal(err)
	}

	return slice, root, nrev, tid
}

func TestDuplicateRecord(t *testing.T) {
	dir, err := os.MkdirTemp("", "integtest")
	if err != nil {
		t.Fatal(err)
	}

	maindb, err := gorm.Open(sqlite.Open(filepath.Join(dir, "test.sqlite")))
	if err != nil {
		t.Fatal(err)
	}
	maindb.AutoMigrate(models.ActorInfo{})

	did := "did:plc:beepboop"
	maindb.Create(&models.ActorInfo{
		Did: did,
		Uid: 1,
	})

	cs := testCarstore(t, dir)

	repoman := NewRepoManager(cs, &util.FakeKeyManager{})

	ctx := context.TODO()
	if err := repoman.InitNewActor(ctx, 1, "hello.world", "did:plc:foobar", "", "", ""); err != nil {
		t.Fatal(err)
	}

	p1, _, err := repoman.CreateRecord(ctx, 1, "app.bsky.feed.post", &bsky.FeedPost{
		Text: fmt.Sprintf("hello friend"),
	})
	if err != nil {
		t.Fatal(err)
	}

	p2, _, err := repoman.CreateRecord(ctx, 1, "app.bsky.feed.post", &bsky.FeedPost{
		Text: fmt.Sprintf("hello friend"),
	})
	if err != nil {
		t.Fatal(err)
	}

	rkey2 := strings.Split(p2, "/")[1]
	if err := repoman.DeleteRecord(ctx, 1, "app.bsky.feed.post", rkey2); err != nil {
		t.Fatal(err)
	}

	rkey1 := strings.Split(p1, "/")[1]
	c, rec, err := repoman.GetRecord(ctx, 1, "app.bsky.feed.post", rkey1, cid.Undef)
	if err != nil {
		t.Fatal(err)
	}

	_ = c
	_ = rec
}
