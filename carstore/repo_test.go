package carstore

import (
	"bytes"
	"context"
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"
	"testing"
	"time"

	sqlbs "github.com/ipfs/go-bs-sqlite3"
	"github.com/ipfs/go-cid"
	flatfs "github.com/ipfs/go-ds-flatfs"
	blockstore "github.com/ipfs/go-ipfs-blockstore"
	"github.com/whyrusleeping/gosky/api"
	"github.com/whyrusleeping/gosky/repo"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func testCarStore() (*CarStore, func(), error) {
	tempdir, err := ioutil.TempDir("", "msttest-")
	if err != nil {
		return nil, nil, err
	}

	sharddir := filepath.Join(tempdir, "shards")
	if err := os.MkdirAll(sharddir, 0775); err != nil {
		return nil, nil, err
	}

	dbstr := "file::memory:"
	//dbstr := filepath.Join(tempdir, "foo.db")
	db, err := gorm.Open(sqlite.Open(dbstr),
		&gorm.Config{
			SkipDefaultTransaction: true,
		})
	if err != nil {
		return nil, nil, err
	}

	cs, err := NewCarStore(db, sharddir)
	if err != nil {
		return nil, nil, err
	}

	return cs, func() {
		_ = os.RemoveAll(tempdir)
	}, nil
}

func testFlatfsBs() (blockstore.Blockstore, func(), error) {
	tempdir, err := ioutil.TempDir("", "msttest-")
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

func TestBasicOperation(t *testing.T) {
	ctx := context.TODO()

	cs, cleanup, err := testCarStore()
	if err != nil {
		t.Fatal(err)
	}
	defer cleanup()

	ds, err := cs.NewDeltaSession(1, cid.Undef)
	if err != nil {
		t.Fatal(err)
	}

	ncid, err := setupRepo(ctx, ds)
	if err != nil {
		t.Fatal(err)
	}

	if err := ds.CloseWithRoot(ctx, ncid); err != nil {
		t.Fatal(err)
	}

	head := ncid
	for i := 0; i < 10; i++ {
		ds, err := cs.NewDeltaSession(1, head)
		if err != nil {
			t.Fatal(err)
		}

		rr, err := repo.OpenRepo(ctx, ds, head)
		if err != nil {
			t.Fatal(err)
		}

		if _, err := rr.CreateRecord(ctx, "app.bsky.feed.post", &api.PostRecord{
			Text: fmt.Sprintf("hey look its a tweet %s", time.Now()),
		}); err != nil {
			t.Fatal(err)
		}

		nroot, err := rr.Commit(ctx)
		if err != nil {
			t.Fatal(err)
		}

		if err := ds.CloseWithRoot(ctx, nroot); err != nil {
			t.Fatal(err)
		}

		head = nroot
	}

	buf := new(bytes.Buffer)
	if err := cs.ReadUserCar(ctx, 1, cid.Undef, true, buf); err != nil {
		t.Fatal(err)
	}

	fmt.Println(buf.Len())

}

func setupRepo(ctx context.Context, bs blockstore.Blockstore) (cid.Cid, error) {
	nr := repo.NewRepo(ctx, bs)

	if _, err := nr.CreateRecord(ctx, "app.bsky.feed.post", &api.PostRecord{
		Text: fmt.Sprintf("hey look its a tweet %s", time.Now()),
	}); err != nil {
		return cid.Undef, err
	}

	ncid, err := nr.Commit(ctx)
	if err != nil {
		return cid.Undef, err
	}

	return ncid, nil
}

func BenchmarkRepoWritesCarstore(b *testing.B) {
	ctx := context.TODO()

	cs, cleanup, err := testCarStore()
	if err != nil {
		b.Fatal(err)
	}
	defer cleanup()

	ds, err := cs.NewDeltaSession(1, cid.Undef)
	if err != nil {
		b.Fatal(err)
	}

	ncid, err := setupRepo(ctx, ds)
	if err != nil {
		b.Fatal(err)
	}

	if err := ds.CloseWithRoot(ctx, ncid); err != nil {
		b.Fatal(err)
	}

	head := ncid
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		ds, err := cs.NewDeltaSession(1, head)
		if err != nil {
			b.Fatal(err)
		}

		rr, err := repo.OpenRepo(ctx, ds, head)
		if err != nil {
			b.Fatal(err)
		}

		if _, err := rr.CreateRecord(ctx, "app.bsky.feed.post", &api.PostRecord{
			Text: fmt.Sprintf("hey look its a tweet %s", time.Now()),
		}); err != nil {
			b.Fatal(err)
		}

		nroot, err := rr.Commit(ctx)
		if err != nil {
			b.Fatal(err)
		}

		if err := ds.CloseWithRoot(ctx, nroot); err != nil {
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

	ncid, err := setupRepo(ctx, bs)
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

		if _, err := rr.CreateRecord(ctx, "app.bsky.feed.post", &api.PostRecord{
			Text: fmt.Sprintf("hey look its a tweet %s", time.Now()),
		}); err != nil {
			b.Fatal(err)
		}

		nroot, err := rr.Commit(ctx)
		if err != nil {
			b.Fatal(err)
		}

		head = nroot
	}
}

func BenchmarkRepoWritesSqlite(b *testing.B) {
	ctx := context.TODO()

	bs, err := sqlbs.Open("file::memory:", sqlbs.Options{})
	if err != nil {
		b.Fatal(err)
	}

	ncid, err := setupRepo(ctx, bs)
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

		if _, err := rr.CreateRecord(ctx, "app.bsky.feed.post", &api.PostRecord{
			Text: fmt.Sprintf("hey look its a tweet %s", time.Now()),
		}); err != nil {
			b.Fatal(err)
		}

		nroot, err := rr.Commit(ctx)
		if err != nil {
			b.Fatal(err)
		}

		head = nroot
	}
}
