package repomgr

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	bsky "github.com/bluesky-social/indigo/api/bsky"
	"github.com/bluesky-social/indigo/carstore"
	"github.com/bluesky-social/indigo/util"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func setupDb(t testing.TB, p string) *gorm.DB {
	t.Helper()

	db, err := gorm.Open(sqlite.Open(p))
	if err != nil {
		t.Fatal(err)
	}

	if err := db.Exec("PRAGMA journal_mode=WAL;").Error; err != nil {
		t.Fatal(err)
	}

	if err := db.Exec("PRAGMA synchronous=normal;").Error; err != nil {
		t.Fatal(err)
	}

	if err := db.Exec("PRAGMA temp_store=memory;").Error; err != nil {
		t.Fatal(err)
	}

	if err := db.Exec("PRAGMA mmap_size=3000000000;").Error; err != nil {
		t.Fatal(err)
	}

	return db
}

func BenchmarkRepoMgrCreates(b *testing.B) {
	dir, err := os.MkdirTemp("", "integtest")
	if err != nil {
		b.Fatal(err)
	}

	cardb := setupDb(b, filepath.Join(dir, "car.sqlite"))

	cspath := filepath.Join(dir, "carstore")
	if err := os.Mkdir(cspath, 0775); err != nil {
		b.Fatal(err)
	}

	/*
		cs, err := carstore.NewCarStore(cardb, []string{cspath})
		if err != nil {
			b.Fatal(err)
		}
	*/
	cs, err := carstore.NewNonArchivalCarstore(cardb)
	if err != nil {
		b.Fatal(err)
	}

	repoman := NewRepoManager(cs, &util.FakeKeyManager{})
	repoman.noArchive = true

	ctx := context.TODO()
	if err := repoman.InitNewActor(ctx, 1, "hello.world", "did:foo:bar", "catdog", "", ""); err != nil {
		b.Fatal(err)
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _, err = repoman.CreateRecord(ctx, 1, "app.bsky.feed.post", &bsky.FeedPost{
			Text: "cats",
		})
		if err != nil {
			b.Fatal(err)
		}
	}

	fmt.Println(carstore.CacheHits, carstore.CacheMiss)
}
