package labeler

import (
	"os"
	"path/filepath"
	"testing"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func testLabelMaker(t *testing.T) *Server {

	tempdir, err := os.MkdirTemp("", "labelmaker-test-")
	if err != nil {
		t.Fatal(err)
	}

	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{SkipDefaultTransaction: true})
	if err != nil {
		t.Fatal(err)
	}

	repoKeyPath := filepath.Join(tempdir, "labelmaker.key")
	seckey, err := LoadOrCreateKeyFile(repoKeyPath, "auto-labelmaker")
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
		SigningKey: *seckey,
		UserId:     1,
	}

	lm, err := NewServer(db, repoUser, plcURL, blobPdsURL, xrpcProxyURL, xrpcProxyAdminPassword, false)
	if err != nil {
		t.Fatal(err)
	}
	return lm
}

func TestLabelMakerCreation(t *testing.T) {
	lm := testLabelMaker(t)
	_ = lm
}
