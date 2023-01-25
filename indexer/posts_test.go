package indexer

import (
	"io/ioutil"
	"os"
	"path/filepath"
	"testing"
	"time"

	bsky "github.com/bluesky-social/indigo/api/bsky"
	"github.com/bluesky-social/indigo/carstore"
	"github.com/bluesky-social/indigo/events"
	"github.com/bluesky-social/indigo/notifs"
	"github.com/bluesky-social/indigo/plc"
	"github.com/bluesky-social/indigo/repomgr"
	"github.com/bluesky-social/indigo/util"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

type testIx struct {
	dir string

	ix *Indexer

	didr plc.PLCClient
}

func testIndexer(t *testing.T) *testIx {
	t.Helper()

	dir, err := ioutil.TempDir("", "ixtest")
	if err != nil {
		t.Fatal(err)
	}

	maindb, err := gorm.Open(sqlite.Open(filepath.Join(dir, "test.db")))
	if err != nil {
		t.Fatal(err)
	}

	cardb, err := gorm.Open(sqlite.Open(filepath.Join(dir, "car.db")))
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

	repoman := repomgr.NewRepoManager(maindb, cs)
	notifman := notifs.NewNotificationManager(maindb, repoman.GetRecord)
	evtman := events.NewEventManager()

	didr := testPLC(t)

	ix, err := NewIndexer(maindb, notifman, evtman, didr)
	if err != nil {
		t.Fatal(err)
	}

	return &testIx{
		dir: dir,
		ix:  ix,

		didr: didr,
	}
}

func (ix *testIx) Cleanup() {
	if ix.dir != "" {
		_ = os.RemoveAll(ix.dir)
	}
}

// TODO: dedupe this out into some testing utility package
func testPLC(t *testing.T) *plc.FakeDid {
	// TODO: just do in memory...
	tdir, err := ioutil.TempDir("", "plcserv")
	if err != nil {
		t.Fatal(err)
	}

	db, err := gorm.Open(sqlite.Open(filepath.Join(tdir, "plc.db")))
	if err != nil {
		t.Fatal(err)
	}
	return plc.NewFakeDid(db)
}

func TestBasicIndexing(t *testing.T) {
	tt := testIndexer(t)
	defer tt.Cleanup()

	post := bsky.FeedPost{
		CreatedAt: time.Now().Format(util.ISO8601),
		Text:      "im the OP, the best",
	}

	_ = post
}
