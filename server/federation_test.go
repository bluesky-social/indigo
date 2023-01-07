package schemagen

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/lestrrat-go/jwx/v2/jwk"
	"github.com/whyrusleeping/gosky/api"
	atproto "github.com/whyrusleeping/gosky/api/atproto"
	bsky "github.com/whyrusleeping/gosky/api/bsky"
	"github.com/whyrusleeping/gosky/carstore"
	"github.com/whyrusleeping/gosky/xrpc"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func makeKey(t *testing.T, fname string) {
	raw, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
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
	plc    *api.PLCServer

	host string

	shutdown func()
}

func (tp *testPDS) Cleanup() {
	if tp.shutdown != nil {
		tp.shutdown()
	}

	if tp.dir != "" {
		_ = os.RemoveAll(tp.dir)
	}
}

func setupPDS(t *testing.T, host, suffix string) *testPDS {
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

	plc := &api.PLCServer{
		Host: "http://localhost:2582",
	}

	srv, err := NewServer(maindb, cs, kfile, suffix, host, plc, []byte(host+suffix))
	if err != nil {
		t.Fatal(err)
	}

	return &testPDS{
		dir:    dir,
		server: srv,
		host:   host,
	}
}

func (tp *testPDS) Run(t *testing.T) {
	// TODO: rig this up so it t.Fatals if the RunAPI call fails immediately
	go func() {
		if err := tp.server.RunAPI(tp.host); err != nil {
			fmt.Println(err)
		}
	}()
	time.Sleep(time.Millisecond * 10)

	tp.shutdown = func() {
		tp.server.echo.Shutdown(context.TODO())
	}
}

type testUser struct {
	handle string
	pds    *testPDS
	did    string

	client *xrpc.Client
}

func (tp *testPDS) PeerWith(t *testing.T, op *testPDS) {
	panic("no")
}

func (tp *testPDS) NewUser(t *testing.T, handle string) *testUser {
	ctx := context.TODO()

	c := &xrpc.Client{
		Host: "http://" + tp.host,
	}

	out, err := atproto.AccountCreate(ctx, c, &atproto.AccountCreate_Input{
		Email:    handle + "@fake.com",
		Handle:   handle,
		Password: "password",
	})
	if err != nil {
		t.Fatal(err)
	}

	c.Auth = &xrpc.AuthInfo{
		AccessJwt:  out.AccessJwt,
		RefreshJwt: out.RefreshJwt,
		Handle:     out.Handle,
		Did:        out.Did,
	}

	return &testUser{
		pds:    tp,
		handle: out.Handle,
		client: c,
		did:    out.Did,
	}
}

func (u *testUser) Post(t *testing.T, body string) string {
	t.Helper()

	ctx := context.TODO()
	resp, err := atproto.RepoCreateRecord(ctx, u.client, &atproto.RepoCreateRecord_Input{
		Collection: "app.bsky.feed.post",
		Did:        u.did,
		Record: &bsky.FeedPost{
			CreatedAt: time.Now().Format(time.RFC3339),
			Text:      body,
		},
	})

	if err != nil {
		t.Fatal(err)
	}

	return resp.Uri
}

func TestBasicFederation(t *testing.T) {
	p1 := setupPDS(t, "localhost:8812", ".pdsone")
	p2 := setupPDS(t, "localhost:8813", ".pdstwo")

	defer p1.Cleanup()
	defer p2.Cleanup()

	p1.Run(t)
	p2.Run(t)

	bob := p1.NewUser(t, "bob.pdsone")
	laura := p2.NewUser(t, "laura.pdstwo")

	//p1.PeerWith(p2)
	bob.Post(t, "hello world")
	laura.Post(t, "hello bob")

}
