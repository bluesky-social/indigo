package diskpersist

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"sync"
	"testing"
	"time"

	atproto "github.com/bluesky-social/indigo/api/atproto"
	"github.com/bluesky-social/indigo/cmd/relay/events"
	"github.com/bluesky-social/indigo/cmd/relay/models"
	lexutil "github.com/bluesky-social/indigo/lex/util"
	"github.com/bluesky-social/indigo/util"
	cid "github.com/ipfs/go-cid"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"

	blocks "github.com/ipfs/go-block-format"
)

func testPersister(t *testing.T) {
	ctx := context.Background()
	db, tempPath, err := setupDBs(t)
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tempPath)

	// Initialize a persister
	dp, err := NewDiskPersistence(filepath.Join(tempPath, "diskPrimary"), filepath.Join(tempPath, "diskArchive"), db, &DiskPersistOptions{
		EventsPerFile: 10,
		UIDCacheSize:  100000,
		DIDCacheSize:  100000,
	})
	if err != nil {
		t.Fatal(err)
	}
	var users testUidSource
	users.Add()
	dp.SetUidSource(&users)

	dp.SetEventBroadcaster(broadcastDiscard)
	// Create a bunch of events

	n := 100
	inEvts := make([]*events.XRPCStreamEvent, n)
	nb := blocks.NewBlock([]byte(fmt.Sprintf("fake block %d", 0)))
	someCid := nb.Cid()
	cidLink := lexutil.LexLink(someCid)
	headLink := lexutil.LexLink(someCid)
	for i := 0; i < n; i++ {
		inEvts[i] = &events.XRPCStreamEvent{
			RepoCommit: &atproto.SyncSubscribeRepos_Commit{
				Repo:   users.dids[0],
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
		err = dp.Persist(ctx, inEvts[i])
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

	dp2, err := NewDiskPersistence(filepath.Join(tempPath, "diskPrimary"), filepath.Join(tempPath, "diskArchive"), db, &DiskPersistOptions{
		EventsPerFile: 10,
		UIDCacheSize:  100000,
		DIDCacheSize:  100000,
	})
	if err != nil {
		t.Fatal(err)
	}
	dp2.SetUidSource(&users)
	dp2.SetEventBroadcaster(broadcastDiscard)

	evtman2 := events.NewEventManager(dp2)

	inEvts = make([]*events.XRPCStreamEvent, n)
	for i := 0; i < n; i++ {
		inEvts[i] = &events.XRPCStreamEvent{
			RepoCommit: &atproto.SyncSubscribeRepos_Commit{
				Repo:   users.dids[0],
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
func TestDiskPersistPlain(t *testing.T) {
	testPersister(t)
}

func BenchmarkDiskPersist(b *testing.B) {
	db, tempPath, err := setupDBs(b)
	if err != nil {
		b.Fatal(err)
	}

	defer os.RemoveAll(tempPath)

	// Initialize a DBPersister

	dp, err := NewDiskPersistence(filepath.Join(tempPath, "diskPrimary"), filepath.Join(tempPath, "diskArchive"), db, &DiskPersistOptions{
		EventsPerFile: 5000,
		UIDCacheSize:  100000,
		DIDCacheSize:  100000,
	})
	if err != nil {
		b.Fatal(err)
	}

	runPersisterBenchmark(b, db, dp)

}

func runPersisterBenchmark(b *testing.B, db *gorm.DB, p events.EventPersistence) {
	ctx := context.Background()

	// Create a bunch of events
	evtman := events.NewEventManager(p)

	var xcid cid.Cid
	xcid.UnmarshalText([]byte("fake cid link"))
	cidLink := lexutil.LexLink(xcid)
	var headCid cid.Cid
	headCid.UnmarshalText([]byte("fake repo head"))
	headLink := lexutil.LexLink(headCid)

	inEvts := make([]*events.XRPCStreamEvent, b.N)
	for i := 0; i < b.N; i++ {
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
				err := evtman.AddEvent(ctx, inEvts[i])
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
	db, tempPath, err := setupDBs(t)
	if err != nil {
		t.Fatal(err)
	}

	defer os.RemoveAll(tempPath)

	// Initialize a DBPersister

	dp, err := NewDiskPersistence(filepath.Join(tempPath, "diskPrimary"), filepath.Join(tempPath, "diskArchive"), db, &DiskPersistOptions{
		EventsPerFile: 20,
		UIDCacheSize:  100000,
		DIDCacheSize:  100000,
	})
	if err != nil {
		t.Fatal(err)
	}

	runEventManagerTest(t, db, dp)
}

func runEventManagerTest(t *testing.T, db *gorm.DB, p events.EventPersistence) {
	ctx := context.Background()

	var users testUidSource
	users.Add()
	evtman := events.NewEventManager(p)
	if uidsourcer, ok := p.(epUidSourcer); ok {
		uidsourcer.SetUidSource(&users)
	}

	nb := blocks.NewBlock([]byte(fmt.Sprintf("fake block %d", 0)))
	someCid := nb.Cid()
	cidLink := lexutil.LexLink(someCid)
	headLink := lexutil.LexLink(someCid)

	testSize := 100 // you can adjust this number as needed
	inEvts := make([]*events.XRPCStreamEvent, testSize)
	for i := 0; i < testSize; i++ {
		inEvts[i] = &events.XRPCStreamEvent{
			RepoCommit: &atproto.SyncSubscribeRepos_Commit{
				Repo:   users.dids[0],
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

		err := evtman.AddEvent(ctx, inEvts[i])
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
		// Clear cache, don't care if one has it and not the other
		inEvts[outEvtCount].Preserialized = nil
		evt.Preserialized = nil
		if !reflect.DeepEqual(inEvts[outEvtCount], evt) {
			t.Logf("%v", inEvts[outEvtCount].RepoCommit)
			t.Logf("%v", evt.RepoCommit)
			t.Fatalf("Event content mismatch: expected %+v, got %+v", inEvts[outEvtCount], evt)
		}
		outEvtCount++
		return nil
	})

	if outEvtCount != testSize {
		t.Fatalf("expected %d events, got %d", testSize, outEvtCount)
	}
}

func TestDiskPersisterTakedowns(t *testing.T) {
	db, tempPath, err := setupDBs(t)
	if err != nil {
		t.Fatal(err)
	}

	defer os.RemoveAll(tempPath)

	// Initialize a DBPersister

	dp, err := NewDiskPersistence(filepath.Join(tempPath, "diskPrimary"), filepath.Join(tempPath, "diskArchive"), db, &DiskPersistOptions{
		EventsPerFile: 10,
		UIDCacheSize:  100000,
		DIDCacheSize:  100000,
	})
	if err != nil {
		t.Fatal(err)
	}

	runTakedownTest(t, db, dp)
}

type testUidSource struct {
	uids []models.Uid
	dids []string
}

func (tus *testUidSource) Add() {
	i := len(tus.uids)
	tus.uids = append(tus.uids, models.Uid(uint(i+1)))
	tus.dids = append(tus.dids, fmt.Sprintf("did:TEST:%d", i+1))
}

var testErrNotFound = errors.New("not found")

func (t testUidSource) DidToUid(ctx context.Context, did string) (models.Uid, error) {
	for i := range t.dids {
		if t.dids[i] == did {
			return t.uids[i], nil
		}
	}
	return 0, testErrNotFound
}

type epUidSourcer interface {
	SetUidSource(uids UidSource)
}

func broadcastDiscard(_ *events.XRPCStreamEvent) {
}

func runTakedownTest(t *testing.T, db *gorm.DB, p events.EventPersistence) {
	ctx := context.TODO()

	p.SetEventBroadcaster(broadcastDiscard)

	// Create multiple users
	const userCount = 10

	uids := make([]models.Uid, userCount)
	dids := make([]string, userCount)
	for i := 0; i < userCount; i++ {
		uids[i] = models.Uid(uint(i + 1))
		dids[i] = fmt.Sprintf("did:TEST:%d", i+1)
	}

	if uidsourcer, ok := p.(epUidSourcer); ok {
		uidsourcer.SetUidSource(&testUidSource{
			uids: uids,
			dids: dids,
		})
	}

	testSize := 100 // you can adjust this number as needed
	inEvts := make([]*events.XRPCStreamEvent, testSize*userCount)
	for i := 0; i < testSize*userCount; i++ {
		ui := i % userCount

		nb := blocks.NewBlock([]byte(fmt.Sprintf("fake block %d", i)))
		someCid := nb.Cid()
		cidLink := lexutil.LexLink(someCid)
		headLink := lexutil.LexLink(someCid)

		inEvts[i] = &events.XRPCStreamEvent{
			RepoCommit: &atproto.SyncSubscribeRepos_Commit{
				Repo:   dids[ui],
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

		err := p.Persist(ctx, inEvts[i])
		if err != nil {
			t.Fatal(err)
		}
	}

	// Flush manually
	if err := p.Flush(ctx); err != nil {
		t.Fatal(err)
	}

	// Pick a user to take down
	const takeDownI = userCount / 2

	err := p.TakeDownRepo(ctx, uids[takeDownI])
	if err != nil {
		t.Fatal(err)
	}

	// Verify that the events of the user have been removed from the event stream
	var evtsCount int
	if err := p.Playback(ctx, 0, func(evt *events.XRPCStreamEvent) error {
		evtsCount++
		if evt.RepoCommit.Repo == dids[takeDownI] {
			t.Fatalf("found event for user %d after takedown", uids[takeDownI])
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}

	exp := testSize * (userCount - 1)
	if evtsCount != exp {
		t.Fatalf("wrong number of events out: %d != %d", evtsCount, exp)
	}
}

func setupDBs(t testing.TB) (*gorm.DB, string, error) {
	dir, err := os.MkdirTemp("", "integtest")
	if err != nil {
		return nil, "", err
	}

	maindb, err := gorm.Open(sqlite.Open(":memory:?cache=shared&mode=rwc"))
	if err != nil {
		return nil, "", err
	}

	tx := maindb.Exec("PRAGMA journal_mode=WAL;")
	if tx.Error != nil {
		return nil, "", tx.Error
	}

	tx.Commit()

	return maindb, dir, nil
}
