package events

import (
	"context"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	atproto "github.com/bluesky-social/indigo/api/atproto"
	"github.com/bluesky-social/indigo/api/bsky"
	"github.com/bluesky-social/indigo/carstore"
	lexutil "github.com/bluesky-social/indigo/lex/util"
	"github.com/bluesky-social/indigo/models"
	pds "github.com/bluesky-social/indigo/pds/data"
	"github.com/bluesky-social/indigo/repomgr"
	"github.com/bluesky-social/indigo/util"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func BenchmarkDBPersist(b *testing.B) {
	ctx := context.Background()

	db, _, cs, tempPath, err := setupDBs(b)
	if err != nil {
		b.Fatal(err)
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
		b.Fatal(err)
	}

	_, cid, err := mgr.CreateRecord(ctx, 1, "app.bsky.feed.post", &bsky.FeedPost{
		Text:      "hello world",
		CreatedAt: time.Now().Format(util.ISO8601),
	})
	if err != nil {
		b.Fatal(err)
	}

	defer os.RemoveAll(tempPath)

	// Initialize a DBPersister
	dbp, err := NewDbPersistence(db, cs, nil)
	if err != nil {
		b.Fatal(err)
	}

	// Create a bunch of events
	evtman := NewEventManager(dbp)

	userRepoHead, err := mgr.GetRepoRoot(ctx, 1)
	if err != nil {
		b.Fatal(err)
	}

	inEvts := make([]*XRPCStreamEvent, b.N)
	for i := 0; i < b.N; i++ {
		cidLink := lexutil.LexLink(cid)
		headLink := lexutil.LexLink(userRepoHead)
		inEvts[i] = &XRPCStreamEvent{
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

	numRoutines := 5
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

	outEvtCount := 0
	expectedEvtCount := b.N * numRoutines

	// Flush manually
	err = dbp.Flush(ctx)
	if err != nil {
		b.Fatal(err)
	}

	b.StopTimer()

	dbp.Playback(ctx, 0, func(evt *XRPCStreamEvent) error {
		outEvtCount++
		return nil
	})

	if outEvtCount != expectedEvtCount {
		b.Fatalf("expected %d events, got %d", expectedEvtCount, outEvtCount)
	}
}

func BenchmarkPlayback(b *testing.B) {
	ctx := context.Background()

	n := b.N

	db, _, cs, tempPath, err := setupDBs(b)
	if err != nil {
		b.Fatal(err)
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
		b.Fatal(err)
	}

	_, cid, err := mgr.CreateRecord(ctx, 1, "app.bsky.feed.post", &bsky.FeedPost{
		Text:      "hello world",
		CreatedAt: time.Now().Format(util.ISO8601),
	})
	if err != nil {
		b.Fatal(err)
	}

	defer os.RemoveAll(tempPath)

	// Initialize a DBPersister
	dbp, err := NewDbPersistence(db, cs, nil)
	if err != nil {
		b.Fatal(err)
	}

	// Create a bunch of events
	evtman := NewEventManager(dbp)

	userRepoHead, err := mgr.GetRepoRoot(ctx, 1)
	if err != nil {
		b.Fatal(err)
	}

	inEvts := make([]*XRPCStreamEvent, n)
	for i := 0; i < n; i++ {
		cidLink := lexutil.LexLink(cid)
		headLink := lexutil.LexLink(userRepoHead)
		inEvts[i] = &XRPCStreamEvent{
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

	numRoutines := 5
	wg := sync.WaitGroup{}

	errChan := make(chan error, numRoutines)

	// Add events in parallel
	for i := 0; i < numRoutines; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < n; i++ {
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

	outEvtCount := 0
	expectedEvtCount := n * numRoutines

	// Flush manually
	err = dbp.Flush(ctx)
	if err != nil {
		b.Fatal(err)
	}

	b.ResetTimer()

	dbp.Playback(ctx, 0, func(evt *XRPCStreamEvent) error {
		outEvtCount++
		return nil
	})

	b.StopTimer()

	if outEvtCount != expectedEvtCount {
		b.Fatalf("expected %d events, got %d", expectedEvtCount, outEvtCount)
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
