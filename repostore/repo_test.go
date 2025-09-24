package repostore

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/bluesky-social/indigo/api/bsky"
	appbsky "github.com/bluesky-social/indigo/api/bsky"
	carstore "github.com/bluesky-social/indigo/carstore"
	"github.com/bluesky-social/indigo/repo"
	"github.com/bluesky-social/indigo/util"

	//sqlbs "github.com/ipfs/go-bs-sqlite3"
	"github.com/ipfs/go-cid"
	flatfs "github.com/ipfs/go-ds-flatfs"
	blockstore "github.com/ipfs/go-ipfs-blockstore"
	ipld "github.com/ipfs/go-ipld-format"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func testCarStore(t testing.TB) (carstore.CarStore, func(), error) {
	tempdir, err := os.MkdirTemp("", "msttest-")
	if err != nil {
		return nil, nil, err
	}

	sharddir1 := filepath.Join(tempdir, "shards1")
	if err := os.MkdirAll(sharddir1, 0775); err != nil {
		return nil, nil, err
	}

	sharddir2 := filepath.Join(tempdir, "shards2")
	if err := os.MkdirAll(sharddir2, 0775); err != nil {
		return nil, nil, err
	}

	dbstr := "file::memory:"
	//dbstr := filepath.Join(tempdir, "foo.sqlite")
	db, err := gorm.Open(sqlite.Open(dbstr),
		&gorm.Config{
			SkipDefaultTransaction: true,
		})
	if err != nil {
		return nil, nil, err
	}

	cs, err := NewRepoStore(db, []string{sharddir1, sharddir2})
	if err != nil {
		return nil, nil, err
	}

	return cs, func() {
		_ = os.RemoveAll(tempdir)
	}, nil
}

type testFactory func(t testing.TB) (carstore.CarStore, func(), error)

var backends = map[string]testFactory{
	"cartore": testCarStore,
}

func testFlatfsBs() (blockstore.Blockstore, func(), error) {
	tempdir, err := os.MkdirTemp("", "msttest-")
	if err != nil {
		return nil, nil, err
	}

	ffds, err := flatfs.CreateOrOpen(tempdir, flatfs.IPFS_DEF_SHARD, false)
	if err != nil {
		return nil, nil, err
	}

	bs := blockstore.NewBlockstoreNoPrefix(ffds)

	return bs, func() {
		_ = os.RemoveAll(tempdir)
	}, nil
}

func TestBasicOperation(ot *testing.T) {
	ctx := context.TODO()

	for fname, tf := range backends {
		ot.Run(fname, func(t *testing.T) {

			cs, cleanup, err := tf(t)
			if err != nil {
				t.Fatal(err)
			}
			defer cleanup()

			ds, err := cs.NewDeltaSession(ctx, 1, nil)
			if err != nil {
				t.Fatal(err)
			}

			ncid, rev, err := setupRepo(ctx, ds, false)
			if err != nil {
				t.Fatal(err)
			}

			if _, err := ds.CloseWithRoot(ctx, ncid, rev); err != nil {
				t.Fatal(err)
			}

			var recs []cid.Cid
			head := ncid
			for i := 0; i < 10; i++ {
				ds, err := cs.NewDeltaSession(ctx, 1, &rev)
				if err != nil {
					t.Fatal(err)
				}

				rr, err := repo.OpenRepo(ctx, ds, head)
				if err != nil {
					t.Fatal(err)
				}

				rc, _, err := rr.CreateRecord(ctx, "app.bsky.feed.post", &appbsky.FeedPost{
					Text: fmt.Sprintf("hey look its a tweet %d", time.Now().UnixNano()),
				})
				if err != nil {
					t.Fatal(err)
				}

				recs = append(recs, rc)

				kmgr := &util.FakeKeyManager{}
				nroot, nrev, err := rr.Commit(ctx, kmgr.SignForUser)
				if err != nil {
					t.Fatal(err)
				}

				rev = nrev

				if err := ds.CalcDiff(ctx, nil); err != nil {
					t.Fatal(err)
				}

				if _, err := ds.CloseWithRoot(ctx, nroot, rev); err != nil {
					t.Fatal(err)
				}

				head = nroot
			}

			buf := new(bytes.Buffer)
			if err := cs.ReadUserCar(ctx, 1, "", true, buf); err != nil {
				t.Fatal(err)
			}
			checkRepo(t, cs, buf, recs)

			if _, err := cs.CompactUserShards(ctx, 1, false); err != nil {
				t.Log(err)
				// TODO:
				//t.Fatal(err)
			}

			buf = new(bytes.Buffer)
			if err := cs.ReadUserCar(ctx, 1, "", true, buf); err != nil {
				t.Fatal(err)
			}
			checkRepo(t, cs, buf, recs)
		})
	}
}

func checkRepo(t *testing.T, cs carstore.CarStore, r io.Reader, expRecs []cid.Cid) {
	t.Helper()
	rep, err := repo.ReadRepoFromCar(context.TODO(), r)
	if err != nil {
		t.Fatal("Reading repo: ", err)
	}

	set := make(map[cid.Cid]bool)
	for _, c := range expRecs {
		set[c] = true
	}

	if err := rep.ForEach(context.TODO(), "", func(k string, v cid.Cid) error {
		if !set[v] {
			return fmt.Errorf("have record we did not expect")
		}

		delete(set, v)
		return nil

	}); err != nil {
		var ierr ipld.ErrNotFound
		if errors.As(err, &ierr) {
			fmt.Println("matched error")
			bs, err := cs.ReadOnlySession(1)
			if err != nil {
				fmt.Println("could not read session: ", err)
			}

			blk, err := bs.Get(context.TODO(), ierr.Cid)
			if err != nil {
				fmt.Println("also failed the local get: ", err)
			} else {
				fmt.Println("LOCAL GET SUCCESS", len(blk.RawData()))
			}
		}

		t.Fatal("walking repo: ", err)
	}

	if len(set) > 0 {
		t.Fatalf("expected to find more cids in repo: %v", set)
	}

}

func setupRepo(ctx context.Context, bs blockstore.Blockstore, mkprofile bool) (cid.Cid, string, error) {
	nr := repo.NewRepo(ctx, "did:foo", bs)

	if mkprofile {
		_, err := nr.PutRecord(ctx, "app.bsky.actor.profile/self", &bsky.ActorProfile{})
		if err != nil {
			return cid.Undef, "", fmt.Errorf("write record failed: %w", err)
		}
	}

	kmgr := &util.FakeKeyManager{}
	ncid, rev, err := nr.Commit(ctx, kmgr.SignForUser)
	if err != nil {
		return cid.Undef, "", fmt.Errorf("commit failed: %w", err)
	}

	return ncid, rev, nil
}

func BenchmarkRepoWritesCarstore(b *testing.B) {
	ctx := context.TODO()

	cs, cleanup, err := testCarStore(b)
	innerBenchmarkRepoWritesCarstore(b, ctx, cs, cleanup, err)
}

func innerBenchmarkRepoWritesCarstore(b *testing.B, ctx context.Context, cs carstore.CarStore, cleanup func(), err error) {
	if err != nil {
		b.Fatal(err)
	}
	defer cleanup()

	ds, err := cs.NewDeltaSession(ctx, 1, nil)
	if err != nil {
		b.Fatal(err)
	}

	ncid, rev, err := setupRepo(ctx, ds, false)
	if err != nil {
		b.Fatal(err)
	}

	if _, err := ds.CloseWithRoot(ctx, ncid, rev); err != nil {
		b.Fatal(err)
	}

	head := ncid
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		ds, err := cs.NewDeltaSession(ctx, 1, &rev)
		if err != nil {
			b.Fatal(err)
		}

		rr, err := repo.OpenRepo(ctx, ds, head)
		if err != nil {
			b.Fatal(err)
		}

		if _, _, err := rr.CreateRecord(ctx, "app.bsky.feed.post", &appbsky.FeedPost{
			Text: fmt.Sprintf("hey look its a tweet %s", time.Now()),
		}); err != nil {
			b.Fatal(err)
		}

		kmgr := &util.FakeKeyManager{}
		nroot, nrev, err := rr.Commit(ctx, kmgr.SignForUser)
		if err != nil {
			b.Fatal(err)
		}

		rev = nrev
		if err := ds.CalcDiff(ctx, nil); err != nil {
			b.Fatal(err)
		}

		if _, err := ds.CloseWithRoot(ctx, nroot, rev); err != nil {
			b.Fatal(err)
		}

		head = nroot
	}
}

func BenchmarkRepoWritesFlatfs(b *testing.B) {
	ctx := context.TODO()

	bs, cleanup, err := testFlatfsBs()
	if err != nil {
		b.Fatal(err)
	}
	defer cleanup()

	ncid, _, err := setupRepo(ctx, bs, false)
	if err != nil {
		b.Fatal(err)
	}

	head := ncid
	b.ResetTimer()
	for i := 0; i < b.N; i++ {

		rr, err := repo.OpenRepo(ctx, bs, head)
		if err != nil {
			b.Fatal(err)
		}

		if _, _, err := rr.CreateRecord(ctx, "app.bsky.feed.post", &appbsky.FeedPost{
			Text: fmt.Sprintf("hey look its a tweet %s", time.Now()),
		}); err != nil {
			b.Fatal(err)
		}

		kmgr := &util.FakeKeyManager{}
		nroot, _, err := rr.Commit(ctx, kmgr.SignForUser)
		if err != nil {
			b.Fatal(err)
		}

		head = nroot
	}
}

/* NOTE(bnewbold): this depends on github.com/ipfs/go-bs-sqlite3, which rewrote git history (?) breaking the dependency tree. We can roll forward, but that will require broad dependency updates. So for now just removing this benchmark/perf test.
func BenchmarkRepoWritesSqlite(b *testing.B) {
	ctx := context.TODO()

	bs, err := sqlbs.Open("file::memory:", sqlbs.Options{})
	if err != nil {
		b.Fatal(err)
	}

	ncid, _, err := setupRepo(ctx, bs, false)
	if err != nil {
		b.Fatal(err)
	}

	head := ncid
	b.ResetTimer()
	for i := 0; i < b.N; i++ {

		rr, err := repo.OpenRepo(ctx, bs, head)
		if err != nil {
			b.Fatal(err)
		}

		if _, _, err := rr.CreateRecord(ctx, "app.bsky.feed.post", &appbsky.FeedPost{
			Text: fmt.Sprintf("hey look its a tweet %s", time.Now()),
		}); err != nil {
			b.Fatal(err)
		}

		kmgr := &util.FakeKeyManager{}
		nroot, _, err := rr.Commit(ctx, kmgr.SignForUser)
		if err != nil {
			b.Fatal(err)
		}

		head = nroot
	}
}
*/

type testWriter struct {
	t testing.TB
}

func (tw testWriter) Write(p []byte) (n int, err error) {
	tw.t.Log(string(p))
	return len(p), nil
}

func slogForTest(t testing.TB) *slog.Logger {
	hopts := slog.HandlerOptions{
		Level: slog.LevelDebug,
	}
	return slog.New(slog.NewTextHandler(&testWriter{t}, &hopts))
}
