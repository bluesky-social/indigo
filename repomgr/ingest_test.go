package repomgr

import (
	"context"
	"io/ioutil"
	"os"
	"path/filepath"
	"testing"

	"github.com/bluesky-social/indigo/carstore"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func skipIfNoFile(t *testing.T, f string) {
	t.Helper()
	_, err := os.Stat(f)
	if err != nil {
		if os.IsNotExist(err) {
			t.Skipf("test vector %s not present, skipping for now", f)
		}

		t.Fatal(err)
	}
}

func TestLoadNewRepo(t *testing.T) {
	skipIfNoFile(t, "testrepo.car")

	dir, err := ioutil.TempDir("", "integtest")
	if err != nil {
		t.Fatal(err)
	}

	maindb, err := gorm.Open(sqlite.Open(filepath.Join(dir, "test.db")))
	if err != nil {
		t.Fatal(err)
	}

	cardb, err := gorm.Open(sqlite.Open(filepath.Join(dir, "car.db")))
	if err != nil {
		t.Fatal(err)
	}

	cspath := filepath.Join(dir, "carstore")
	if err := os.Mkdir(cspath, 0775); err != nil {
		t.Fatal(err)
	}

	cs, err := carstore.NewCarStore(cardb, cspath)
	if err != nil {
		t.Fatal(err)
	}

	repoman := NewRepoManager(maindb, cs)

	fi, err := os.Open("testrepo.car")
	if err != nil {
		t.Fatal(err)
	}

	ctx := context.TODO()
	if err := repoman.ImportNewRepo(ctx, 2, fi); err != nil {
		t.Fatal(err)
	}
}
