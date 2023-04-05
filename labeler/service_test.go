package labeler

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/bluesky-social/indigo/carstore"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func testLabelMaker(t *testing.T) *Server {

	tempdir, err := os.MkdirTemp("", "labelmaker-test-")
	if err != nil {
		t.Fatal(err)
	}
	sharddir := filepath.Join(tempdir, "shards")
	if err := os.MkdirAll(sharddir, 0775); err != nil {
		t.Fatal(err)
	}

	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{SkipDefaultTransaction: true})
	if err != nil {
		t.Fatal(err)
	}

	cs, err := carstore.NewCarStore(db, sharddir)
	if err != nil {
		t.Fatal(err)
	}

	repoKeyPath := filepath.Join(tempdir, "labelmaker.key")
	serkey, err := LoadOrCreateKeyFile(repoKeyPath, "auto-labelmaker")
	if err != nil {
		t.Fatal(err)
	}

	plcURL := "http://did-plc-test.dummy"
	blobPdsURL := "http://pds-test.dummy"
	xrpcProxyURL := "http://pds-test.dummy"
	xrpcProxyAdminPassword := "xrpc-test-password"
	repoUser := RepoConfig{
		Handle:     "test.handle.dummy",
		Did:        "did:plc:testdummy",
		Password:   "admin-test-password",
		SigningKey: serkey,
		UserId:     1,
	}

	lm, err := NewServer(db, cs, repoUser, plcURL, blobPdsURL, xrpcProxyURL, xrpcProxyAdminPassword, false)
	if err != nil {
		t.Fatal(err)
	}
	return lm
}

func TestLabelMakerCreation(t *testing.T) {
	lm := testLabelMaker(t)
	_ = lm
}
