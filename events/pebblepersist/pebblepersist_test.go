package pebblepersist

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	atproto "github.com/bluesky-social/indigo/api/atproto"
	"github.com/bluesky-social/indigo/api/bsky"
	"github.com/bluesky-social/indigo/carstore"
	"github.com/bluesky-social/indigo/events"
	"github.com/bluesky-social/indigo/events/diskpersist"
	"github.com/bluesky-social/indigo/events/eventmgr"
	lexutil "github.com/bluesky-social/indigo/lex/util"
	"github.com/bluesky-social/indigo/models"
	pds "github.com/bluesky-social/indigo/pds/data"
	"github.com/bluesky-social/indigo/repomgr"
	"github.com/bluesky-social/indigo/util"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func TestPebblePersist(t *testing.T) {
	factory := func(tempPath string, db *gorm.DB) (events.EventPersistence, error) {
		opts := DefaultPebblePersistOptions
		opts.DbPath = filepath.Join(tempPath, "pebble.db")
		return NewPebblePersistance(&opts)
	}
	testPersister(t, factory)
}

func testPersister(t *testing.T, perisistenceFactory func(path string, db *gorm.DB) (events.EventPersistence, error)) {
	ctx := context.Background()

	db, _, cs, tempPath, err := setupDBs(t)
	if err != nil {
		t.Fatal(err)
	}

	db.AutoMigrate(&pds.User{})
	db.AutoMigrate(&pds.Peering{})
	db.AutoMigrate(&models.ActorInfo{})

	db.Create(&models.ActorInfo{
		Uid: 1,
		Did: "did:example:123",
	})

	mgr := repomgr.NewRepoManager(cs, &util.FakeKeyManager{})

	err = mgr.InitNewActor(ctx, 1, "alice", "did:example:123", "Alice", "", "")
	if err != nil {
		t.Fatal(err)
	}

	_, cid, err := mgr.CreateRecord(ctx, 1, "app.bsky.feed.post", &bsky.FeedPost{
		Text:      "hello world",
		CreatedAt: time.Now().Format(util.ISO8601),
	})
	if err != nil {
		t.Fatal(err)
	}

	defer os.RemoveAll(tempPath)

	// Initialize a persister
	dp, err := perisistenceFactory(tempPath, db)
	if err != nil {
		t.Fatal(err)
	}

	// Create a bunch of events
	evtman := eventmgr.NewEventManager(dp)

	userRepoHead, err := mgr.GetRepoRoot(ctx, 1)
	if err != nil {
		t.Fatal(err)
	}

	n := 100
	inEvts := make([]*events.XRPCStreamEvent, n)
	for i := 0; i < n; i++ {
		cidLink := lexutil.LexLink(cid)
		headLink := lexutil.LexLink(userRepoHead)
		inEvts[i] = &events.XRPCStreamEvent{
			RepoCommit: &atproto.SyncSubscribeRepos_Commit{
				Repo:   "did:example:123",
				Commit: headLink,
				Ops: []*atproto.SyncSubscribeRepos_RepoOp{
					{
						Action: "add",
						Cid:    &cidLink,
						Path:   "path1",
					},
				},
				Time: time.Now().Format(util.ISO8601),
				Seq:  int64(i),
			},
		}
	}

	// Add events in parallel
	for i := 0; i < n; i++ {
		err = evtman.AddEvent(ctx, inEvts[i])
		if err != nil {
			t.Fatal(err)
		}
	}

	if err := dp.Flush(ctx); err != nil {
		t.Fatal(err)
	}

	outEvtCount := 0
	expectedEvtCount := n

	dp.Playback(ctx, 0, func(evt *events.XRPCStreamEvent) error {
		outEvtCount++
		return nil
	})

	if outEvtCount != expectedEvtCount {
		t.Fatalf("expected %d events, got %d", expectedEvtCount, outEvtCount)
	}

	dp.Shutdown(ctx)

	time.Sleep(time.Millisecond * 100)

	dp2, err := diskpersist.NewDiskPersistence(filepath.Join(tempPath, "diskPrimary"), filepath.Join(tempPath, "diskArchive"), db, &diskpersist.DiskPersistOptions{
		EventsPerFile: 10,
		UIDCacheSize:  100000,
		DIDCacheSize:  100000,
	})
	if err != nil {
		t.Fatal(err)
	}

	evtman2 := eventmgr.NewEventManager(dp2)

	inEvts = make([]*events.XRPCStreamEvent, n)
	for i := 0; i < n; i++ {
		cidLink := lexutil.LexLink(cid)
		headLink := lexutil.LexLink(userRepoHead)
		inEvts[i] = &events.XRPCStreamEvent{
			RepoCommit: &atproto.SyncSubscribeRepos_Commit{
				Repo:   "did:example:123",
				Commit: headLink,
				Ops: []*atproto.SyncSubscribeRepos_RepoOp{
					{
						Action: "add",
						Cid:    &cidLink,
						Path:   "path1",
					},
				},
				Time: time.Now().Format(util.ISO8601),
			},
		}
	}

	for i := 0; i < n; i++ {
		err = evtman2.AddEvent(ctx, inEvts[i])
		if err != nil {
			t.Fatal(err)
		}
	}
}

func setupDBs(t testing.TB) (*gorm.DB, *gorm.DB, carstore.CarStore, string, error) {
	dir, err := os.MkdirTemp("", "integtest")
	if err != nil {
		return nil, nil, nil, "", err
	}

	maindb, err := gorm.Open(sqlite.Open(filepath.Join(dir, "test.sqlite?cache=shared&mode=rwc")))
	if err != nil {
		return nil, nil, nil, "", err
	}

	tx := maindb.Exec("PRAGMA journal_mode=WAL;")
	if tx.Error != nil {
		return nil, nil, nil, "", tx.Error
	}

	tx.Commit()

	cardb, err := gorm.Open(sqlite.Open(filepath.Join(dir, "car.sqlite?cache=shared&mode=rwc")))
	if err != nil {
		return nil, nil, nil, "", err
	}

	tx = cardb.Exec("PRAGMA journal_mode=WAL;")
	if tx.Error != nil {
		return nil, nil, nil, "", tx.Error
	}

	cspath := filepath.Join(dir, "carstore")
	if err := os.Mkdir(cspath, 0775); err != nil {
		return nil, nil, nil, "", err
	}

	cs, err := carstore.NewCarStore(cardb, []string{cspath})
	if err != nil {
		return nil, nil, nil, "", err
	}

	return maindb, cardb, cs, dir, nil
}
