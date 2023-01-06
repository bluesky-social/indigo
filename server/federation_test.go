package schemagen

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"
	"testing"

	"github.com/lestrrat-go/jwx/v2/jwk"
	"github.com/whyrusleeping/gosky/carstore"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func makeKey(t *testing.T, fname string) {
	raw, err := ecdsa.GenerateKey(elliptic.P384(), rand.Reader)
	if err != nil {
		t.Fatal(fmt.Errorf("failed to generate new ECDSA private key: %s", err))
	}

	key, err := jwk.FromRaw(raw)
	if err != nil {
		t.Fatal(fmt.Errorf("failed to create ECDSA key: %s", err))
	}

	if _, ok := key.(jwk.ECDSAPrivateKey); !ok {
		t.Fatal(fmt.Errorf("expected jwk.ECDSAPrivateKey, got %T", key))
	}

	key.Set(jwk.KeyIDKey, "mykey")

	buf, err := json.MarshalIndent(key, "", "  ")
	if err != nil {
		t.Fatal(fmt.Errorf("failed to marshal key into JSON: %w", err))
	}

	if err := os.WriteFile(fname, buf, 0664); err != nil {
		t.Fatal(err)
	}

}

type testPDS struct {
	dir    string
	server *Server
}

func setupPDS(t *testing.T, suffix string) *testPDS {
	dir, err := ioutil.TempDir("", "fedtest")
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

	kfile := filepath.Join(dir, "server.key")
	makeKey(t, kfile)

	srv, err := NewServer(maindb, cs, kfile, suffix)
	if err != nil {
		t.Fatal(err)
	}

	return &testPDS{
		dir:    dir,
		server: srv,
	}
}

func TestBasicFederation(t *testing.T) {

}
