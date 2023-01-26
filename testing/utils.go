package testing

import (
	"bytes"
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"net/http"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/bluesky-social/indigo/api"
	atproto "github.com/bluesky-social/indigo/api/atproto"
	bsky "github.com/bluesky-social/indigo/api/bsky"
	"github.com/bluesky-social/indigo/bgs"
	"github.com/bluesky-social/indigo/carstore"
	"github.com/bluesky-social/indigo/events"
	"github.com/bluesky-social/indigo/indexer"
	"github.com/bluesky-social/indigo/lex/util"
	"github.com/bluesky-social/indigo/notifs"
	"github.com/bluesky-social/indigo/plc"
	"github.com/bluesky-social/indigo/repomgr"
	server "github.com/bluesky-social/indigo/server"
	"github.com/bluesky-social/indigo/xrpc"
	"github.com/lestrrat-go/jwx/v2/jwk"

	"github.com/gorilla/websocket"
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
	server *server.Server
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

func setupPDS(t *testing.T, host, suffix string, plc plc.PLCClient) *testPDS {
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

	kfile := filepath.Join(dir, "server.key")
	makeKey(t, kfile)

	srv, err := server.NewServer(maindb, cs, kfile, suffix, host, plc, []byte(host+suffix))
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
		tp.server.Shutdown(context.TODO())
	}
}

func (tp *testPDS) RequestScraping(t *testing.T, b *testBGS) {
	t.Helper()
	bb, err := json.Marshal(map[string]string{"host": tp.host})
	if err != nil {
		t.Fatal(err)
	}

	resp, err := http.Post("http://"+b.host+"/add-target", "application/json", bytes.NewReader(bb))
	if err != nil {
		t.Fatal(err)
	}

	if resp.StatusCode != 200 {
		t.Fatal("invalid response from bgs", resp.StatusCode)
	}
}

type testUser struct {
	handle string
	pds    *testPDS
	did    string

	client *xrpc.Client
}

/*
func (tp *testPDS) PeerWith(t *testing.T, op *testPDS) {
	if err := tp.server.HackAddPeering(op.host, op.server.signingKey.DID()); err != nil {
		t.Fatal(err)
	}

	if err := op.server.HackAddPeering(tp.host, tp.server.signingKey.DID()); err != nil {
		t.Fatal(err)
	}
}
*/

func (tp *testPDS) NewUser(t *testing.T, handle string) *testUser {
	ctx := context.TODO()

	c := &xrpc.Client{
		Host: "http://" + tp.host,
	}

	fmt.Println("HOST: ", c.Host)
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
		Record: util.LexiconTypeDecoder{&bsky.FeedPost{
			CreatedAt: time.Now().Format(time.RFC3339),
			Text:      body,
			Reply: &bsky.FeedPost_ReplyRef{
				Parent: &atproto.RepoStrongRef{
					Cid: pcid,
					Uri: post,
				},
			}},
		},
	})

	if err != nil {
		t.Fatal(err)
	}

	return resp.Uri
}

func (u *testUser) DID() string {
	return u.did
}

func (u *testUser) Post(t *testing.T, body string) *atproto.RepoStrongRef {
	t.Helper()

	ctx := context.TODO()
	resp, err := atproto.RepoCreateRecord(ctx, u.client, &atproto.RepoCreateRecord_Input{
		Collection: "app.bsky.feed.post",
		Did:        u.did,
		Record: util.LexiconTypeDecoder{&bsky.FeedPost{
			CreatedAt: time.Now().Format(time.RFC3339),
			Text:      body,
		}},
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
		Record: util.LexiconTypeDecoder{&bsky.GraphFollow{
			CreatedAt: time.Now().Format(time.RFC3339),
			Subject: &bsky.ActorRef{
				DeclarationCid: "bafyreid27zk7lbis4zw5fz4podbvbs4fc5ivwji3dmrwa6zggnj4bnd57u",
				Did:            did,
			},
		}},
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

func testPLC(t *testing.T) *plc.FakeDid {
	// TODO: just do in memory...
	tdir, err := ioutil.TempDir("", "plcserv")
	if err != nil {
		t.Fatal(err)
	}

	db, err := gorm.Open(sqlite.Open(filepath.Join(tdir, "plc.db")))
	if err != nil {
		t.Fatal(err)
	}
	return plc.NewFakeDid(db)
}

type testBGS struct {
	bgs  *bgs.BGS
	host string
}

func setupBGS(t *testing.T, host string, didr plc.PLCClient) *testBGS {
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

	repoman := repomgr.NewRepoManager(maindb, cs)

	notifman := notifs.NewNotificationManager(maindb, repoman.GetRecord)

	evtman := events.NewEventManager()

	go evtman.Run()

	ix, err := indexer.NewIndexer(maindb, notifman, evtman, didr)
	if err != nil {
		t.Fatal(err)
	}

	repoman.SetEventHandler(func(ctx context.Context, evt *repomgr.RepoEvent) {
		if err := ix.HandleRepoEvent(ctx, evt); err != nil {
			fmt.Println("test bgs failed to handle repo event", err)
		}
	})

	b := bgs.NewBGS(maindb, ix, repoman, evtman, didr)

	return &testBGS{
		bgs:  b,
		host: host,
	}
}

func (b *testBGS) Run(t *testing.T) {
	go func() {
		if err := b.bgs.Start(b.host); err != nil {
			fmt.Println(err)
		}
	}()
	time.Sleep(time.Millisecond * 10)
}

type eventStream struct {
	lk     sync.Mutex
	events []*events.RepoEvent
	cancel func()

	cur int
}

func (b *testBGS) Events(t *testing.T, since int64) *eventStream {
	d := websocket.Dialer{}
	h := http.Header{}

	if since >= 0 {
		h.Set("since", fmt.Sprint(since))
	}

	con, resp, err := d.Dial("ws://"+b.host+"/events", h)
	if err != nil {
		t.Fatal(err)
	}

	if resp.StatusCode != 101 {
		t.Fatal("expected http 101 response, got: ", resp.StatusCode)
	}

	ctx, cancel := context.WithCancel(context.Background())

	es := &eventStream{
		cancel: cancel,
	}

	go func() {
		<-ctx.Done()
		con.Close()
	}()

	go func() {
		for {
			mt, r, err := con.NextReader()
			if err != nil {
				panic(err)
			}

			switch mt {
			default:
				panic("We are reallly not prepared for this")
			case websocket.BinaryMessage:
				// ok
			}

			var header events.EventHeader
			if err := header.UnmarshalCBOR(r); err != nil {
				panic(err)
			}

			switch header.Type {
			case "data":
				var evt events.RepoEvent
				if err := evt.UnmarshalCBOR(r); err != nil {
					panic(err)
				}

				es.lk.Lock()
				es.events = append(es.events, &evt)
				es.lk.Unlock()
			default:
				panic(fmt.Sprintf("unrecognized event stream type: %q", header.Type))
			}

		}
	}()

	return es
}

func (es *eventStream) Next() *events.RepoEvent {
	defer es.lk.Unlock()
	for {
		es.lk.Lock()
		if len(es.events) > es.cur {
			es.cur++
			return es.events[es.cur-1]
		}
		es.lk.Unlock()
		time.Sleep(time.Millisecond * 10)
	}
}

func (es *eventStream) All() []*events.RepoEvent {
	es.lk.Lock()
	defer es.lk.Unlock()
	out := make([]*events.RepoEvent, len(es.events))
	for i, e := range es.events {
		out[i] = e
	}

	return out
}

/*
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

	fmt.Println("LAURA POST!!!!")
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
	if len(lnot) != 1 {
		t.Fatal("wrong number of notifications")
	}

}
*/
