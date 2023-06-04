package events_test

import (
	"bytes"
	"context"
	"fmt"
	"math/rand"
	"os"
	"path/filepath"
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
	"github.com/bluesky-social/indigo/repo"
	"github.com/bluesky-social/indigo/repomgr"
	intTesting "github.com/bluesky-social/indigo/testing"
	"github.com/bluesky-social/indigo/util"
	"github.com/ipfs/go-cid"
	"github.com/ipfs/go-log/v2"
	car "github.com/ipld/go-car"
	"github.com/stretchr/testify/assert"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func init() {
	log.SetAllLoggers(log.LevelDebug)
}

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

	mgr := repomgr.NewRepoManager(db, cs, &util.FakeKeyManager{})

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
	dbp, err := events.NewDbPersistence(db, cs, nil)
	if err != nil {
		b.Fatal(err)
	}

	// Create a bunch of events
	evtman := events.NewEventManager(dbp)

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
	err = dbp.FlushBatch(ctx)
	if err != nil {
		b.Fatal(err)
	}

	b.StopTimer()

	dbp.Playback(ctx, 0, func(evt *events.XRPCStreamEvent) error {
		outEvtCount++
		return nil
	})

	if outEvtCount != expectedEvtCount {
		b.Fatalf("expected %d events, got %d", expectedEvtCount, outEvtCount)
	}
}

func TestDBPersist(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping BGS test in 'short' test mode")
	}
	assert := assert.New(t)
	didr := intTesting.TestPLC(t)
	p1 := intTesting.MustSetupPDS(t, "localhost:5155", ".tpds", didr)
	p1.Run(t)

	b1 := intTesting.MustSetupBGS(t, "localhost:8231", didr)
	b1.Run(t)

	p1.RequestScraping(t, b1)

	time.Sleep(time.Millisecond * 50)

	evts := b1.Events(t, -1)
	defer evts.Cancel()

	bob := p1.MustNewUser(t, "bob.tpds")
	alice := p1.MustNewUser(t, "alice.tpds")

	bp1 := bob.Post(t, "cats for cats")
	ap1 := alice.Post(t, "no i like dogs")

	_ = bp1
	_ = ap1

	fmt.Println("bob:", bob.DID())
	fmt.Println("alice:", alice.DID())

	fmt.Println("event 1")
	e1 := evts.Next()
	assert.NotNil(e1.RepoCommit)
	assert.Equal(e1.RepoCommit.Repo, bob.DID())

	fmt.Println("event 2")
	e2 := evts.Next()
	assert.NotNil(e2.RepoCommit)
	assert.Equal(e2.RepoCommit.Repo, alice.DID())

	fmt.Println("event 3")
	e3 := evts.Next()
	assert.Equal(e3.RepoCommit.Repo, bob.DID())
	//assert.Equal(e3.RepoCommit.Ops[0].Kind, "createRecord")

	fmt.Println("event 4")
	e4 := evts.Next()
	assert.Equal(e4.RepoCommit.Repo, alice.DID())
	//assert.Equal(e4.RepoCommit.Ops[0].Kind, "createRecord")

	// playback
	pbevts := b1.Events(t, 2)
	defer pbevts.Cancel()

	fmt.Println("event 5")
	pbe1 := pbevts.Next()
	assert.Equal(*e3, *pbe1)
}

func setupDBs(t testing.TB) (*gorm.DB, *gorm.DB, *carstore.CarStore, string, error) {
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

	cardb, err := gorm.Open(sqlite.Open(filepath.Join(dir, "car.sqlite")))
	if err != nil {
		return nil, nil, nil, "", err
	}

	cspath := filepath.Join(dir, "carstore")
	if err := os.Mkdir(cspath, 0775); err != nil {
		return nil, nil, nil, "", err
	}

	cs, err := carstore.NewCarStore(cardb, cspath)
	if err != nil {
		return nil, nil, nil, "", err
	}

	return maindb, cardb, cs, "", nil
}

func randomFollows(t *testing.T, users []*intTesting.TestUser) {
	for n := 0; n < 3; n++ {
		for i, u := range users {
			oi := rand.Intn(len(users))
			if i == oi {
				continue
			}

			u.Follow(t, users[oi].DID())
		}
	}
}

func socialSim(t *testing.T, users []*intTesting.TestUser, postiter, likeiter int) []*atproto.RepoStrongRef {
	var posts []*atproto.RepoStrongRef
	for i := 0; i < postiter; i++ {
		for _, u := range users {
			posts = append(posts, u.Post(t, intTesting.MakeRandomPost()))
		}
	}

	for i := 0; i < likeiter; i++ {
		for _, u := range users {
			u.Like(t, posts[rand.Intn(len(posts))])
		}
	}

	return posts
}

func commitFromSlice(t *testing.T, slice []byte, rcid cid.Cid) *repo.SignedCommit {
	carr, err := car.NewCarReader(bytes.NewReader(slice))
	if err != nil {
		t.Fatal(err)
	}

	for {
		blk, err := carr.Next()
		if err != nil {
			t.Fatal(err)
		}

		if blk.Cid() == rcid {

			var sc repo.SignedCommit
			if err := sc.UnmarshalCBOR(bytes.NewReader(blk.RawData())); err != nil {
				t.Fatal(err)
			}
			return &sc
		}
	}
}
