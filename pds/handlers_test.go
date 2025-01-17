package pds

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"os"
	"path/filepath"
	"testing"

	"github.com/bluesky-social/indigo/api/atproto"
	"github.com/bluesky-social/indigo/carstore"
	"github.com/bluesky-social/indigo/plc"
	"github.com/bluesky-social/indigo/util/cliutil"
	"github.com/whyrusleeping/go-did"
	"gorm.io/gorm"
)

func testCarStore(t *testing.T, db *gorm.DB) (carstore.CarStore, func()) {
	t.Helper()
	tempdir, err := os.MkdirTemp("", "msttest-")
	if err != nil {
		t.Fatal(err)
	}

	sharddir := filepath.Join(tempdir, "shards")
	if err := os.MkdirAll(sharddir, 0775); err != nil {
		t.Fatal(err)
	}

	cs, err := carstore.NewCarStore(db, []string{sharddir})
	if err != nil {
		t.Fatal(err)
	}

	return cs, func() { _ = os.RemoveAll(tempdir) }
}

func newTestServer(t *testing.T) (*Server, func()) {
	t.Helper()
	db, err := cliutil.SetupDatabase("sqlite://:memory:", 40)
	if err != nil {
		t.Fatal(err)
	}
	cs, cleanup := testCarStore(t, db)
	fakePlc := plc.NewFakeDid(db)
	raw, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	serkey := &did.PrivKey{
		Raw:  raw,
		Type: did.KeyTypeP256,
	}

	s, err := NewServer(db, cs, serkey, ".test", "", fakePlc, []byte("jwtsecretplaceholder"))
	if err != nil {
		t.Fatal(err)
	}
	return s, func() {
		cleanup()
		sqlDB, err := db.DB()
		if err != nil {
			t.Fatal(err)
		}
		err = sqlDB.Close()
		if err != nil {
			t.Fatal(err)
		}
	}
}

func TestHandleComAtprotoAccountCreate(t *testing.T) {
	s, cleanup := newTestServer(t)
	defer cleanup()

	e := "test@foo.com"
	p := "password"
	o, err := s.handleComAtprotoServerCreateAccount(context.Background(), &atproto.ServerCreateAccount_Input{
		Email:    &e,
		Password: &p,
		Handle:   "testman.test",
	})
	if err != nil {
		t.Fatal(err)
	}
	u, err := s.lookupUserByDid(context.Background(), o.Did)
	if err != nil {
		t.Fatal(err)
	}
	if u.Email != "test@foo.com" {
		t.Fatal("user has different email")
	}

}

func TestHandleComAtprotoSessionCreate(t *testing.T) {
	s, cleanup := newTestServer(t)
	defer cleanup()

	e := "test@foo.com"
	p := "password"
	o, err := s.handleComAtprotoServerCreateAccount(context.Background(), &atproto.ServerCreateAccount_Input{
		Email:    &e,
		Password: &p,
		Handle:   "testman.test",
	})
	if err != nil {
		t.Fatal(err)
	}
	so, err := s.handleComAtprotoServerCreateSession(context.Background(), &atproto.ServerCreateSession_Input{
		Identifier: o.Handle,
		Password:   "password",
	})
	if err != nil {
		t.Fatal(err)
	}
	if so.Handle != o.Handle {
		t.Fatal("user has different handle")
	}

	_, err = s.handleComAtprotoServerCreateSession(context.Background(), &atproto.ServerCreateSession_Input{
		Identifier: o.Handle,
		Password:   "invalid",
	})
	if err != ErrInvalidUsernameOrPassword {
		t.Fatalf("expected error %s, got %s\n", ErrInvalidUsernameOrPassword, err)
	}
}
