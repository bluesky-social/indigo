package backfill_test

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/bluesky-social/indigo/backfill"
	"github.com/ipfs/go-cid"
	typegen "github.com/whyrusleeping/cbor-gen"
	"go.uber.org/zap"
)

var logger *zap.SugaredLogger

type testState struct {
	creates int
	updates int
	deletes int
	lk      sync.Mutex
}

func TestBackfill(t *testing.T) {
	ctx := context.Background()

	testRepos := []string{
		"did:plc:q6gjnaw2blty4crticxkmujt",
		"did:plc:f5f4diimystr7ima7nqvamhe",
		"did:plc:t7y4sud4dhptvzz7ibnv5cbt",
	}

	mem := backfill.NewMemstore()

	rawLog, err := zap.NewDevelopment()
	if err != nil {
		t.Fatal(err)
	}

	logger = rawLog.Sugar()

	ts := &testState{}

	opts := backfill.DefaultBackfillOptions()
	opts.NSIDFilter = "app.bsky.feed.follow/"

	bf := backfill.NewBackfiller(
		"backfill-test",
		mem,
		ts.handleCreate,
		ts.handleUpdate,
		ts.handleDelete,
		logger,
		opts,
	)

	logger.Info("starting backfiller")

	go bf.Start()

	for _, repo := range testRepos {
		mem.EnqueueJob(repo)
	}

	// Wait until job 0 is in progress
	for {
		s, err := mem.GetJob(ctx, testRepos[0])
		if err != nil {
			t.Fatal(err)
		}
		if s.State() == backfill.StateInProgress {
			mem.BufferOp(ctx, testRepos[0], "delete", "app.bsky.feed.follow/1", nil, &cid.Undef)
			mem.BufferOp(ctx, testRepos[0], "delete", "app.bsky.feed.follow/2", nil, &cid.Undef)
			mem.BufferOp(ctx, testRepos[0], "delete", "app.bsky.feed.follow/3", nil, &cid.Undef)
			mem.BufferOp(ctx, testRepos[0], "delete", "app.bsky.feed.follow/4", nil, &cid.Undef)
			mem.BufferOp(ctx, testRepos[0], "delete", "app.bsky.feed.follow/5", nil, &cid.Undef)

			mem.BufferOp(ctx, testRepos[0], "create", "app.bsky.feed.follow/1", nil, &cid.Undef)

			mem.BufferOp(ctx, testRepos[0], "update", "app.bsky.feed.follow/1", nil, &cid.Undef)

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

	logger.Infof("shutting down")
}

func (ts *testState) handleCreate(ctx context.Context, repo string, path string, rec *typegen.CBORMarshaler, cid *cid.Cid) error {
	logger.Infof("got create: %s %s", repo, path)
	ts.lk.Lock()
	ts.creates++
	ts.lk.Unlock()
	return nil
}

func (ts *testState) handleUpdate(ctx context.Context, repo string, path string, rec *typegen.CBORMarshaler, cid *cid.Cid) error {
	logger.Infof("got update: %s %s", repo, path)
	ts.lk.Lock()
	ts.updates++
	ts.lk.Unlock()
	return nil
}

func (ts *testState) handleDelete(ctx context.Context, repo string, path string) error {
	logger.Infof("got delete: %s %s", repo, path)
	ts.lk.Lock()
	ts.deletes++
	ts.lk.Unlock()
	return nil
}
