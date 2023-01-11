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
	"github.com/stretchr/testify/assert"
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

func setupPDS(t *testing.T, host, suffix string, plc PLCClient) *testPDS {
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
	if err := tp.server.HackAddPeering(op.host, op.server.signingKey.DID()); err != nil {
		t.Fatal(err)
	}

	if err := op.server.HackAddPeering(tp.host, tp.server.signingKey.DID()); err != nil {
		t.Fatal(err)
	}
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

func (u *testUser) Reply(t *testing.T, post, pcid, body string) string {
	t.Helper()

	ctx := context.TODO()
	resp, err := atproto.RepoCreateRecord(ctx, u.client, &atproto.RepoCreateRecord_Input{
		Collection: "app.bsky.feed.post",
		Did:        u.did,
		Record: &bsky.FeedPost{
			CreatedAt: time.Now().Format(time.RFC3339),
			Text:      body,
			Reply: &bsky.FeedPost_ReplyRef{
				Parent: &atproto.RepoStrongRef{
					Cid: pcid,
					Uri: post,
				},
			},
		},
	})

	if err != nil {
		t.Fatal(err)
	}

	return resp.Uri
}
func (u *testUser) Post(t *testing.T, body string) *atproto.RepoStrongRef {
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

	return &atproto.RepoStrongRef{
		Cid: resp.Cid,
		Uri: resp.Uri,
	}
}

func (u *testUser) Follow(t *testing.T, did string) string {
	t.Helper()

	ctx := context.TODO()
	resp, err := atproto.RepoCreateRecord(ctx, u.client, &atproto.RepoCreateRecord_Input{
		Collection: "app.bsky.graph.follow",
		Did:        u.did,
		Record: &bsky.GraphFollow{
			CreatedAt: time.Now().Format(time.RFC3339),
			Subject: &bsky.ActorRef{
				DeclarationCid: "bafyreid27zk7lbis4zw5fz4podbvbs4fc5ivwji3dmrwa6zggnj4bnd57u",
				Did:            did,
			},
		},
	})

	if err != nil {
		t.Fatal(err)
	}

	return resp.Uri
}

func (u *testUser) GetFeed(t *testing.T) []*bsky.FeedFeedViewPost {
	t.Helper()

	ctx := context.TODO()
	resp, err := bsky.FeedGetTimeline(ctx, u.client, "reverse-chronlogical", "", 100)
	if err != nil {
		t.Fatal(err)
	}

	return resp.Feed
}

func (u *testUser) GetNotifs(t *testing.T) []*bsky.NotificationList_Notification {
	t.Helper()

	ctx := context.TODO()
	resp, err := bsky.NotificationList(ctx, u.client, "", 100)
	if err != nil {
		t.Fatal(err)
	}

	return resp.Notifications
}

func testPLC(t *testing.T) *FakeDid {
	// TODO: just do in memory...
	tdir, err := ioutil.TempDir("", "plcserv")
	if err != nil {
		t.Fatal(err)
	}

	db, err := gorm.Open(sqlite.Open(filepath.Join(tdir, "plc.db")))
	if err != nil {
		t.Fatal(err)
	}
	return NewFakeDid(db)

}

func TestBasicFederation(t *testing.T) {
	assert := assert.New(t)
	plc := testPLC(t)
	p1 := setupPDS(t, "0.0.0.0:8812", ".pdsone", plc)
	p2 := setupPDS(t, "0.0.0.0:8813", ".pdstwo", plc)

	defer p1.Cleanup()
	defer p2.Cleanup()

	p1.Run(t)
	p2.Run(t)

	bob := p1.NewUser(t, "bob.pdsone")
	laura := p2.NewUser(t, "laura.pdstwo")

	p1.PeerWith(t, p2)
	bob.Follow(t, laura.did)

	bp1 := bob.Post(t, "hello world")
	lp1 := laura.Post(t, "hello bob")
	time.Sleep(time.Millisecond * 50)

	f := bob.GetFeed(t)
	assert.Equal(f[0].Post.Uri, bp1.Uri)
	assert.Equal(f[1].Post.Uri, lp1.Uri)

	lp2 := laura.Post(t, "im posting again!")
	time.Sleep(time.Millisecond * 50)

	f = bob.GetFeed(t)
	assert.Equal(f[0].Post.Uri, bp1.Uri)
	assert.Equal(f[1].Post.Uri, lp1.Uri)
	assert.Equal(f[2].Post.Uri, lp2.Uri)

	fmt.Println("laura notifications:")
	lnot := laura.GetNotifs(t)
	for _, n := range lnot {
		fmt.Println(n)
	}

	select {}
}
