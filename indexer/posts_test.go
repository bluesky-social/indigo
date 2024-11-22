package indexer

import (
	"context"
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
	rm *repomgr.RepoManager

	didr plc.PLCClient
}

func testIndexer(t *testing.T) *testIx {
	t.Helper()

	dir, err := os.MkdirTemp("", "ixtest")
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

	cs, err := carstore.NewCarStore(cardb, []string{cspath})
	if err != nil {
		t.Fatal(err)
	}

	repoman := repomgr.NewRepoManager(cs, &util.FakeKeyManager{})
	notifman := notifs.NewNotificationManager(maindb, repoman.GetRecord)
	evtman := events.NewEventManager(events.NewMemPersister())

	didr := testPLC(t)

	rf := NewRepoFetcher(maindb, repoman, 10)

	ix, err := NewIndexer(maindb, notifman, evtman, didr, rf, false, true, true)
	if err != nil {
		t.Fatal(err)
	}

	return &testIx{
		dir: dir,
		ix:  ix,
		rm:  repoman,

		didr: didr,
	}
}

func (ix *testIx) Cleanup() {
	if ix.dir != "" {
		_ = os.RemoveAll(ix.dir)
	}
	ix.ix.Shutdown()
}

// TODO: dedupe this out into some testing utility package
func testPLC(t *testing.T) *plc.FakeDid {
	// TODO: just do in memory...
	tdir, err := os.MkdirTemp("", "plcserv")
	if err != nil {
		t.Fatal(err)
	}

	db, err := gorm.Open(sqlite.Open(filepath.Join(tdir, "plc.sqlite")))
	if err != nil {
		t.Fatal(err)
	}
	return plc.NewFakeDid(db)
}

func TestBasicIndexing(t *testing.T) {
	tt := testIndexer(t)
	defer tt.Cleanup()

	post := &bsky.FeedPost{
		CreatedAt: time.Now().Format(util.ISO8601),
		Text:      "im the OP, the best",
	}

	ctx := context.Background()

	if err := tt.rm.InitNewActor(ctx, 1, "bob", "did:plc:asdasda", "bob", "FAKE", "userboy"); err != nil {
		t.Fatal(err)
	}

	uri, cc, err := tt.rm.CreateRecord(ctx, 1, "app.bsky.feed.post", post)
	if err != nil {
		t.Fatal(err)
	}

	_ = uri
	_ = cc

	// TODO: test some things at this level specifically.
	// we have higher level integration tests, but at some point I want to stress this mechanism directly.
	// we will want a decent set of utilities to make it easy on ourselves
	// things to test:
	// - crawling missing data
	// - references to missing posts work
	// - mentions?
}
