package events_test

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"sync"
	"testing"
	"time"

	atproto "github.com/bluesky-social/indigo/api/atproto"
	"github.com/bluesky-social/indigo/api/bsky"
	"github.com/bluesky-social/indigo/carstore"
	"github.com/bluesky-social/indigo/events"
	lexutil "github.com/bluesky-social/indigo/lex/util"
	"github.com/bluesky-social/indigo/models"
	"github.com/bluesky-social/indigo/pds"
	"github.com/bluesky-social/indigo/repomgr"
	"github.com/bluesky-social/indigo/util"
	"gorm.io/gorm"
)

func TestDiskPersist(t *testing.T) {
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

	mgr := repomgr.NewRepoManager(repomgr.NewDbHeadStore(db), cs, &util.FakeKeyManager{})

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

	// Initialize a DBPersister

	dp, err := events.NewDiskPersistence(filepath.Join(tempPath, "diskPrimary"), filepath.Join(tempPath, "diskArchive"), db, &events.DiskPersistOptions{
		EventsPerFile: 10,
		UIDCacheSize:  100000,
		DIDCacheSize:  100000,
	})
	if err != nil {
		t.Fatal(err)
	}

	// Create a bunch of events
	evtman := events.NewEventManager(dp)

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
}

func BenchmarkDiskPersist(b *testing.B) {
	db, _, cs, tempPath, err := setupDBs(b)
	if err != nil {
		b.Fatal(err)
	}

	defer os.RemoveAll(tempPath)

	// Initialize a DBPersister

	dp, err := events.NewDiskPersistence(filepath.Join(tempPath, "diskPrimary"), filepath.Join(tempPath, "diskArchive"), db, &events.DiskPersistOptions{
		EventsPerFile: 5000,
		UIDCacheSize:  100000,
		DIDCacheSize:  100000,
	})
	if err != nil {
		b.Fatal(err)
	}

	runPersisterBenchmark(b, cs, db, dp)
}

func runPersisterBenchmark(b *testing.B, cs *carstore.CarStore, db *gorm.DB, p events.EventPersistence) {
	ctx := context.Background()

	db.AutoMigrate(&pds.User{})
	db.AutoMigrate(&pds.Peering{})
	db.AutoMigrate(&models.ActorInfo{})

	db.Create(&models.ActorInfo{
		Uid: 1,
		Did: "did:example:123",
	})

	mgr := repomgr.NewRepoManager(repomgr.NewDbHeadStore(db), cs, &util.FakeKeyManager{})

	err := mgr.InitNewActor(ctx, 1, "alice", "did:example:123", "Alice", "", "")
	if err != nil {
		b.Fatal(err)
	}

	_, cid, err := mgr.CreateRecord(ctx, 1, "app.bsky.feed.post", &bsky.FeedPost{
		Text:      "hello world",
		CreatedAt: time.Now().Format(util.ISO8601),
	})
	if err != nil {
		b.Fatal(err)
	}

	// Create a bunch of events
	evtman := events.NewEventManager(p)

	userRepoHead, err := mgr.GetRepoRoot(ctx, 1)
	if err != nil {
		b.Fatal(err)
	}

	inEvts := make([]*events.XRPCStreamEvent, b.N)
	for i := 0; i < b.N; i++ {
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

	numRoutines := 4
	wg := sync.WaitGroup{}

	b.ResetTimer()

	errChan := make(chan error, numRoutines)

	// Add events in parallel
	for i := 0; i < numRoutines; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < b.N; i++ {
				err = evtman.AddEvent(ctx, inEvts[i])
				if err != nil {
					errChan <- err
				}
			}
		}()
	}

	wg.Wait()
	close(errChan)

	// Check for errors
	for err := range errChan {
		if err != nil {
			b.Fatal(err)
		}
	}

	// Flush manually
	if err := p.Flush(ctx); err != nil {
		b.Fatal(err)
	}

}

func TestDiskPersister(t *testing.T) {
	db, _, cs, tempPath, err := setupDBs(t)
	if err != nil {
		t.Fatal(err)
	}

	defer os.RemoveAll(tempPath)

	// Initialize a DBPersister

	dp, err := events.NewDiskPersistence(filepath.Join(tempPath, "diskPrimary"), filepath.Join(tempPath, "diskArchive"), db, &events.DiskPersistOptions{
		EventsPerFile: 20,
		UIDCacheSize:  100000,
		DIDCacheSize:  100000,
	})
	if err != nil {
		t.Fatal(err)
	}

	runEventManagerTest(t, cs, db, dp)
}

func runEventManagerTest(t *testing.T, cs *carstore.CarStore, db *gorm.DB, p events.EventPersistence) {
	ctx := context.Background()

	db.AutoMigrate(&pds.User{})
	db.AutoMigrate(&pds.Peering{})
	db.AutoMigrate(&models.ActorInfo{})

	db.Create(&models.ActorInfo{
		Uid: 1,
		Did: "did:example:123",
	})

	mgr := repomgr.NewRepoManager(repomgr.NewDbHeadStore(db), cs, &util.FakeKeyManager{})

	err := mgr.InitNewActor(ctx, 1, "alice", "did:example:123", "Alice", "", "")
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

	evtman := events.NewEventManager(p)

	userRepoHead, err := mgr.GetRepoRoot(ctx, 1)
	if err != nil {
		t.Fatal(err)
	}

	testSize := 100 // you can adjust this number as needed
	inEvts := make([]*events.XRPCStreamEvent, testSize)
	for i := 0; i < testSize; i++ {
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

		err = evtman.AddEvent(ctx, inEvts[i])
		if err != nil {
			t.Fatal(err)
		}
	}

	// Flush manually
	if err := p.Flush(ctx); err != nil {
		t.Fatal(err)
	}

	outEvtCount := 0
	p.Playback(ctx, 0, func(evt *events.XRPCStreamEvent) error {
		// Check that the contents of the output events match the input events
		if !reflect.DeepEqual(inEvts[outEvtCount], evt) {
			t.Fatalf("Event content mismatch: expected %+v, got %+v", inEvts[outEvtCount], evt)
		}
		outEvtCount++
		return nil
	})

	if outEvtCount != testSize {
		t.Fatalf("expected %d events, got %d", testSize, outEvtCount)
	}
}
