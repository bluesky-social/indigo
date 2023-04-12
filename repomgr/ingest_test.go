package repomgr

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"

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

	maindb, err := gorm.Open(sqlite.Open(filepath.Join(dir, "test.sqlite")))
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

	cs, err := carstore.NewCarStore(cardb, cspath)
	if err != nil {
		t.Fatal(err)
	}

	repoman := NewRepoManager(maindb, cs, &util.FakeKeyManager{})

	fi, err := os.Open("../testing/test_files/divy.repo")
	if err != nil {
		t.Fatal(err)
	}
	defer fi.Close()

	ctx := context.TODO()
	if err := repoman.ImportNewRepo(ctx, 2, "", fi, cid.Undef); err != nil {
		t.Fatal(err)
	}
}

func testCarstore(t *testing.T, dir string) *carstore.CarStore {
	cardb, err := gorm.Open(sqlite.Open(filepath.Join(dir, "car.sqlite")))
	if err != nil {
		t.Fatal(err)
	}

	cspath := filepath.Join(dir, "carstore")
	if err := os.Mkdir(cspath, 0775); err != nil {
		t.Fatal(err)
	}

	cs, err := carstore.NewCarStore(cardb, cspath)
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

	repoman := NewRepoManager(maindb, cs, &util.FakeKeyManager{})

	dir2, err := os.MkdirTemp("", "integtest")
	if err != nil {
		t.Fatal(err)
	}
	cs2 := testCarstore(t, dir2)

	ctx := context.TODO()
	var prev *cid.Cid
	for i := 0; i < 5; i++ {
		slice, head := doPost(t, cs2, did, prev, i)

		if err := repoman.HandleExternalUserEvent(ctx, 1, 1, did, prev, slice); err != nil {
			t.Fatal(err)
		}

		prev = &head
	}

	latest := *prev

	// now do a few outside of the standard event stream flow
	for i := 0; i < 5; i++ {
		_, head := doPost(t, cs2, did, prev, i)
		prev = &head
	}

	buf := new(bytes.Buffer)
	if err := cs2.ReadUserCar(ctx, 1, latest, *prev, true, buf); err != nil {
		t.Fatal(err)
	}

	if err := repoman.ImportNewRepo(ctx, 1, did, buf, latest); err != nil {
		t.Fatal(err)
	}
}

func doPost(t *testing.T, cs *carstore.CarStore, did string, prev *cid.Cid, postid int) ([]byte, cid.Cid) {
	ctx := context.TODO()
	ds, err := cs.NewDeltaSession(ctx, 1, prev)
	if err != nil {
		t.Fatal(err)
	}

	r := repo.NewRepo(ctx, did, ds)

	if _, _, err := r.CreateRecord(ctx, "app.bsky.feed.post", &bsky.FeedPost{
		Text: fmt.Sprintf("hello friend %d", postid),
	}); err != nil {
		t.Fatal(err)
	}

	root, err := r.Commit(ctx, func(context.Context, string, []byte) ([]byte, error) { return nil, nil })
	if err != nil {
		t.Fatal(err)
	}

	slice, err := ds.CloseWithRoot(ctx, root)
	if err != nil {
		t.Fatal(err)
	}

	return slice, root
}
