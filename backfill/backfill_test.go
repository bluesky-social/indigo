package backfill_test

import (
	"context"
	"log/slog"
	"sync"
	"testing"

	"github.com/ipfs/go-cid"
	typegen "github.com/whyrusleeping/cbor-gen"
)

type testState struct {
	creates int
	updates int
	deletes int
	lk      sync.Mutex
}

func TestBackfill(t *testing.T) {
	/* this test depends on being able to hit the live production bgs...
	ctx := context.Background()

	testRepos := []string{
		"did:plc:q6gjnaw2blty4crticxkmujt",
		"did:plc:f5f4diimystr7ima7nqvamhe",
		"did:plc:t7y4sud4dhptvzz7ibnv5cbt",
	}

	db, err := gorm.Open(sqlite.Open("sqlite://:memory"))
	if err != nil {
		t.Fatal(err)
	}

	store := backfill.NewGormstore(db)
	ts := &testState{}

	opts := backfill.DefaultBackfillOptions()
	opts.CheckoutPath = "https://bsky.network/xrpc/com.atproto.sync.getRepo"
	opts.NSIDFilter = "app.bsky.feed.follow/"

	bf := backfill.NewBackfiller(
		"backfill-test",
		store,
		ts.handleCreate,
		ts.handleUpdate,
		ts.handleDelete,
		opts,
	)

	slog.Info("starting backfiller")

	go bf.Start()

	for _, repo := range testRepos {
		store.EnqueueJob(repo)
	}

	// Wait until job 0 is in progress
	for {
		s, err := store.GetJob(ctx, testRepos[0])
		if err != nil {
			t.Fatal(err)
		}
		if s.State() == backfill.StateInProgress {
			bf.BufferOp(ctx, testRepos[0], repomgr.EvtKindDeleteRecord, "app.bsky.feed.follow/1", nil, &cid.Undef)
			bf.BufferOp(ctx, testRepos[0], repomgr.EvtKindDeleteRecord, "app.bsky.feed.follow/2", nil, &cid.Undef)
			bf.BufferOp(ctx, testRepos[0], repomgr.EvtKindDeleteRecord, "app.bsky.feed.follow/3", nil, &cid.Undef)
			bf.BufferOp(ctx, testRepos[0], repomgr.EvtKindDeleteRecord, "app.bsky.feed.follow/4", nil, &cid.Undef)
			bf.BufferOp(ctx, testRepos[0], repomgr.EvtKindDeleteRecord, "app.bsky.feed.follow/5", nil, &cid.Undef)

			bf.BufferOp(ctx, testRepos[0], repomgr.EvtKindCreateRecord, "app.bsky.feed.follow/1", nil, &cid.Undef)

			bf.BufferOp(ctx, testRepos[0], repomgr.EvtKindUpdateRecord, "app.bsky.feed.follow/1", nil, &cid.Undef)

			break
		}
		time.Sleep(100 * time.Millisecond)
	}

	for {
		ts.lk.Lock()
		if ts.deletes >= 5 && ts.creates >= 1 && ts.updates >= 1 {
			ts.lk.Unlock()
			break
		}
		ts.lk.Unlock()
		time.Sleep(100 * time.Millisecond)
	}

	bf.Stop()

	slog.Info("shutting down")
	*/
}

func (ts *testState) handleCreate(ctx context.Context, repo string, path string, rec typegen.CBORMarshaler, cid *cid.Cid) error {
	slog.Info("got create", "repo", repo, "path", path)
	ts.lk.Lock()
	ts.creates++
	ts.lk.Unlock()
	return nil
}

func (ts *testState) handleUpdate(ctx context.Context, repo string, path string, rec typegen.CBORMarshaler, cid *cid.Cid) error {
	slog.Info("got update", "repo", repo, "path", path)
	ts.lk.Lock()
	ts.updates++
	ts.lk.Unlock()
	return nil
}

func (ts *testState) handleDelete(ctx context.Context, repo string, path string) error {
	slog.Info("got delete", "repo", repo, "path", path)
	ts.lk.Lock()
	ts.deletes++
	ts.lk.Unlock()
	return nil
}
