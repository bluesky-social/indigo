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

	"github.com/ipfs/go-cid"
	"github.com/whyrusleeping/gosky/api"
	"github.com/whyrusleeping/gosky/repo"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func TestBasicOperation(t *testing.T) {
	tempdir, err := ioutil.TempDir("", "msttest-")
	if err != nil {
		t.Fatal(err)
	}

	defer func() {
		_ = os.RemoveAll(tempdir)
	}()

	sharddir := filepath.Join(tempdir, "shards")
	if err := os.MkdirAll(sharddir, 0775); err != nil {
		t.Fatal(err)
	}

	db, err := gorm.Open(sqlite.Open(filepath.Join(tempdir, "foo.db")))
	if err != nil {
		t.Fatal(err)
	}

	cs, err := NewCarStore(db, sharddir)
	if err != nil {
		t.Fatal(err)
	}

	ds, err := cs.NewDeltaSession(1, cid.Undef)
	if err != nil {
		t.Fatal(err)
	}

	ctx := context.TODO()

	nr := repo.NewRepo(ctx, ds)

	if err := nr.CreateRecord(ctx, "app.bsky.feed.post", &api.PostRecord{
		Text: fmt.Sprintf("hey look its a tweet %s", time.Now()),
	}); err != nil {
		t.Fatal(err)
	}

	ncid, err := nr.Commit(ctx)
	if err != nil {
		t.Fatal(err)
	}

	if err := ds.CloseWithRoot(ctx, ncid); err != nil {
		t.Fatal(err)
	}

	buf := new(bytes.Buffer)
	if err := cs.ReadUserCar(ctx, 1, cid.Undef, true, buf); err != nil {
		t.Fatal(err)
	}
	fmt.Println(buf.Len())

	head := ncid
	for i := 0; i < 10; i++ {
		fmt.Println("head: ", head)
		ds, err := cs.NewDeltaSession(1, head)
		if err != nil {
			t.Fatal(err)
		}

		rr, err := repo.OpenRepo(ctx, ds, head)
		if err != nil {
			t.Fatal(err)
		}

		if err := rr.CreateRecord(ctx, "app.bsky.feed.post", &api.PostRecord{
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

	buf = new(bytes.Buffer)
	if err := cs.ReadUserCar(ctx, 1, cid.Undef, true, buf); err != nil {
		t.Fatal(err)
	}

	fmt.Println(buf.Len())
}
