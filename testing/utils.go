package testing

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"encoding/base32"
	"encoding/json"
	"fmt"
	mathrand "math/rand"
	"net/http"
	"os"
	"path/filepath"
	"strings"
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
	lexutil "github.com/bluesky-social/indigo/lex/util"
	"github.com/bluesky-social/indigo/notifs"
	"github.com/bluesky-social/indigo/pds"
	"github.com/bluesky-social/indigo/plc"
	"github.com/bluesky-social/indigo/repo"
	"github.com/bluesky-social/indigo/repomgr"
	bsutil "github.com/bluesky-social/indigo/util"
	"github.com/bluesky-social/indigo/xrpc"
	"github.com/ipfs/go-cid"
	"github.com/lestrrat-go/jwx/v2/jwk"
	"github.com/multiformats/go-multihash"

	"github.com/gorilla/websocket"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func makeKey(fname string) error {
	raw, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return fmt.Errorf("failed to generate new ECDSA private key: %s", err)
	}

	key, err := jwk.FromRaw(raw)
	if err != nil {
		return fmt.Errorf("failed to create ECDSA key: %s", err)
	}

	if _, ok := key.(jwk.ECDSAPrivateKey); !ok {
		return fmt.Errorf("expected jwk.ECDSAPrivateKey, got %T", key)
	}

	key.Set(jwk.KeyIDKey, "mykey")

	buf, err := json.MarshalIndent(key, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal key into JSON: %w", err)
	}

	if err := os.WriteFile(fname, buf, 0664); err != nil {
		return err
	}

	return nil
}

type testPDS struct {
	dir    string
	server *pds.Server
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

func mustSetupPDS(t *testing.T, host, suffix string, plc plc.PLCClient) *testPDS {
	t.Helper()

	tpds, err := SetupPDS(host, suffix, plc)
	if err != nil {
		t.Fatal(err)
	}

	return tpds
}

func SetupPDS(host, suffix string, plc plc.PLCClient) (*testPDS, error) {
	dir, err := os.MkdirTemp("", "integtest")
	if err != nil {
		return nil, err
	}

	maindb, err := gorm.Open(sqlite.Open(filepath.Join(dir, "test.sqlite")))
	if err != nil {
		return nil, err
	}

	cardb, err := gorm.Open(sqlite.Open(filepath.Join(dir, "car.sqlite")))
	if err != nil {
		return nil, err
	}

	cspath := filepath.Join(dir, "carstore")
	if err := os.Mkdir(cspath, 0775); err != nil {
		return nil, err
	}

	cs, err := carstore.NewCarStore(cardb, cspath)
	if err != nil {
		return nil, err
	}

	kfile := filepath.Join(dir, "server.key")
	if err := makeKey(kfile); err != nil {
		return nil, err
	}

	srv, err := pds.NewServer(maindb, cs, kfile, suffix, host, plc, []byte(host+suffix))
	if err != nil {
		return nil, err
	}

	return &testPDS{
		dir:    dir,
		server: srv,
		host:   host,
	}, nil
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

	c := &xrpc.Client{Host: "http://" + b.host}
	if err := atproto.SyncRequestCrawl(context.TODO(), c, tp.host); err != nil {
		t.Fatal(err)
	}
}

type testUser struct {
	handle string
	pds    *testPDS
	did    string

	client *xrpc.Client
}

func (tp *testPDS) MustNewUser(t *testing.T, handle string) *testUser {
	t.Helper()

	u, err := tp.NewUser(handle)
	if err != nil {
		t.Fatal(err)
	}

	return u
}

func (tp *testPDS) NewUser(handle string) (*testUser, error) {
	ctx := context.TODO()

	c := &xrpc.Client{
		Host: "http://" + tp.host,
	}

	fmt.Println("HOST: ", c.Host)
	out, err := atproto.ServerCreateAccount(ctx, c, &atproto.ServerCreateAccount_Input{
		Email:    handle + "@fake.com",
		Handle:   handle,
		Password: "password",
	})
	if err != nil {
		return nil, err
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
	}, nil
}

func (u *testUser) Reply(t *testing.T, replyto, root *atproto.RepoStrongRef, body string) string {
	t.Helper()

	ctx := context.TODO()
	resp, err := atproto.RepoCreateRecord(ctx, u.client, &atproto.RepoCreateRecord_Input{
		Collection: "app.bsky.feed.post",
		Repo:       u.did,
		Record: lexutil.LexiconTypeDecoder{&bsky.FeedPost{
			CreatedAt: time.Now().Format(time.RFC3339),
			Text:      body,
			Reply: &bsky.FeedPost_ReplyRef{
				Parent: replyto,
				Root:   root,
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
		Repo:       u.did,
		Record: lexutil.LexiconTypeDecoder{&bsky.FeedPost{
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

func (u *testUser) Like(t *testing.T, post *atproto.RepoStrongRef) {
	t.Helper()

	ctx := context.TODO()
	_, err := atproto.RepoCreateRecord(ctx, u.client, &atproto.RepoCreateRecord_Input{
		Collection: "app.bsky.feed.vote",
		Repo:       u.did,
		Record: lexutil.LexiconTypeDecoder{&bsky.FeedLike{
			LexiconTypeID: "app.bsky.feed.vote",
			CreatedAt:     time.Now().Format(time.RFC3339),
			Subject:       post,
		}},
	})
	if err != nil {
		t.Fatal(err)
	}

}

func (u *testUser) Follow(t *testing.T, did string) string {
	t.Helper()

	ctx := context.TODO()
	resp, err := atproto.RepoCreateRecord(ctx, u.client, &atproto.RepoCreateRecord_Input{
		Collection: "app.bsky.graph.follow",
		Repo:       u.did,
		Record: lexutil.LexiconTypeDecoder{&bsky.GraphFollow{
			CreatedAt: time.Now().Format(time.RFC3339),
			Subject:   did,
		}},
	})

	if err != nil {
		t.Fatal(err)
	}

	return resp.Uri
}

func (u *testUser) GetFeed(t *testing.T) []*bsky.FeedDefs_FeedViewPost {
	t.Helper()

	ctx := context.TODO()
	resp, err := bsky.FeedGetTimeline(ctx, u.client, "reverse-chronlogical", "", 100)
	if err != nil {
		t.Fatal(err)
	}

	return resp.Feed
}

func (u *testUser) GetNotifs(t *testing.T) []*bsky.NotificationListNotifications_Notification {
	t.Helper()

	ctx := context.TODO()
	resp, err := bsky.NotificationListNotifications(ctx, u.client, "", 100)
	if err != nil {
		t.Fatal(err)
	}

	return resp.Notifications
}

func testPLC(t *testing.T) *plc.FakeDid {
	// TODO: just do in memory...
	tdir, err := os.MkdirTemp("", "plcserv")
	if err != nil {
		t.Fatal(err)
	}

	db, err := gorm.Open(sqlite.Open(filepath.Join(tdir, "plc.sqlite")))
	if err != nil {
		t.Fatal(err)
	}
	return plc.NewFakeDid(db)
}

type testBGS struct {
	bgs  *bgs.BGS
	host string
}

func mustSetupBGS(t *testing.T, host string, didr plc.PLCClient) *testBGS {
	tbgs, err := SetupBGS(host, didr)
	if err != nil {
		t.Fatal(err)
	}

	return tbgs
}

func SetupBGS(host string, didr plc.PLCClient) (*testBGS, error) {
	dir, err := os.MkdirTemp("", "integtest")
	if err != nil {
		return nil, err
	}

	maindb, err := gorm.Open(sqlite.Open(filepath.Join(dir, "test.sqlite")))
	if err != nil {
		return nil, err
	}

	cardb, err := gorm.Open(sqlite.Open(filepath.Join(dir, "car.sqlite")))
	if err != nil {
		return nil, err
	}

	cspath := filepath.Join(dir, "carstore")
	if err := os.Mkdir(cspath, 0775); err != nil {
		return nil, err
	}

	cs, err := carstore.NewCarStore(cardb, cspath)
	if err != nil {
		return nil, err
	}

	//kmgr := indexer.NewKeyManager(didr, nil)
	kmgr := &bsutil.FakeKeyManager{}

	repoman := repomgr.NewRepoManager(maindb, cs, kmgr)

	notifman := notifs.NewNotificationManager(maindb, repoman.GetRecord)

	evtman := events.NewEventManager(events.NewMemPersister())

	go evtman.Run()

	ix, err := indexer.NewIndexer(maindb, notifman, evtman, didr, repoman, true, true)
	if err != nil {
		return nil, err
	}

	repoman.SetEventHandler(func(ctx context.Context, evt *repomgr.RepoEvent) {
		if err := ix.HandleRepoEvent(ctx, evt); err != nil {
			fmt.Println("test bgs failed to handle repo event", err)
		}
	})

	b, err := bgs.NewBGS(maindb, ix, repoman, evtman, didr, nil, false)
	if err != nil {
		return nil, err
	}

	return &testBGS{
		bgs:  b,
		host: host,
	}, nil
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
	events []*events.XRPCStreamEvent
	cancel func()

	cur int
}

func (b *testBGS) Events(t *testing.T, since int64) *eventStream {
	d := websocket.Dialer{}
	h := http.Header{}

	q := ""
	if since >= 0 {
		q = fmt.Sprintf("?cursor=%d", since)
	}

	con, resp, err := d.Dial("ws://"+b.host+"/xrpc/com.atproto.sync.subscribeRepos"+q, h)
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
		if err := events.HandleRepoStream(ctx, con, &events.RepoStreamCallbacks{
			RepoAppend: func(evt *events.RepoAppend) error {
				fmt.Println("received event: ", evt.Seq, evt.Repo)
				es.lk.Lock()
				es.events = append(es.events, &events.XRPCStreamEvent{RepoAppend: evt})
				es.lk.Unlock()
				return nil
			},
		}); err != nil {
			fmt.Println(err)
		}
	}()

	return es
}

func (es *eventStream) Next() *events.XRPCStreamEvent {
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

func (es *eventStream) All() []*events.XRPCStreamEvent {
	es.lk.Lock()
	defer es.lk.Unlock()
	out := make([]*events.XRPCStreamEvent, len(es.events))
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

var words = []string{
	"cat",
	"is",
	"cash",
	"dog",
	"bad",
	"system",
	"random",
	"skoot",
	"reply",
	"fish",
	"sunshine",
	"bluesky",
	"make",
	"equal",
	"stars",
	"water",
	"parrot",
}

func makeRandomPost() string {
	var out []string
	for i := 0; i < 20; i++ {
		out = append(out, words[mathrand.Intn(len(words))])
	}

	return strings.Join(out, " ")
}

var usernames = []string{
	"alice",
	"bob",
	"carol",
	"darin",
	"eve",
	"francis",
	"gerald",
	"hank",
	"ian",
	"jeremy",
	"karl",
	"louise",
	"marion",
	"nancy",
	"oscar",
	"paul",
	"quentin",
	"raul",
	"serena",
	"trevor",
	"ursula",
	"valerie",
	"walter",
	"xico",
	"yousef",
	"zane",
}

func RandSentence(words []string, maxl int) string {
	var out string
	for {
		w := words[mathrand.Intn(len(words))]
		if len(out)+len(w) >= maxl {
			return out
		}

		out = out + " " + w
	}
}

func ReadWords() ([]string, error) {
	b, err := os.ReadFile("/usr/share/dict/words")
	if err != nil {
		return nil, err
	}

	return strings.Split(string(b), "\n"), nil
}

func RandFakeCid() cid.Cid {
	buf := make([]byte, 32)
	rand.Read(buf)

	pref := cid.NewPrefixV1(cid.DagCBOR, multihash.SHA2_256)
	c, err := pref.Sum(buf)
	if err != nil {
		panic(err)
	}

	return c
}

func RandFakeAtUri(collection, rkey string) string {
	buf := make([]byte, 10)
	rand.Read(buf)
	did := base32.StdEncoding.EncodeToString(buf)

	if rkey == "" {
		rand.Read(buf)
		rkey = base32.StdEncoding.EncodeToString(buf[:6])
	}

	return fmt.Sprintf("at://did:plc:%s/%s/%s", did, collection, rkey)
}

func randAction() string {
	v := mathrand.Intn(100)
	if v < 40 {
		return "post"
	} else if v < 60 {
		return "repost"
	} else if v < 80 {
		return "reply"
	} else {
		return "like"
	}
}

func GenerateFakeRepo(r *repo.Repo, size int) (cid.Cid, error) {
	words, err := ReadWords()
	if err != nil {
		return cid.Undef, err
	}

	ctx := context.TODO()

	var root cid.Cid
	for i := 0; i < size; i++ {
		switch randAction() {
		case "post":
			_, _, err := r.CreateRecord(ctx, "app.bsky.feed.post", &bsky.FeedPost{
				CreatedAt: time.Now().Format(bsutil.ISO8601),
				Text:      RandSentence(words, 200),
			})
			if err != nil {
				return cid.Undef, err
			}
		case "repost":
			_, _, err := r.CreateRecord(ctx, "app.bsky.feed.repost", &bsky.FeedRepost{
				CreatedAt: time.Now().Format(bsutil.ISO8601),
				Subject: &atproto.RepoStrongRef{
					Uri: RandFakeAtUri("app.bsky.feed.post", ""),
					Cid: RandFakeCid().String(),
				},
			})
			if err != nil {
				return cid.Undef, err
			}
		case "reply":
			_, _, err := r.CreateRecord(ctx, "app.bsky.feed.post", &bsky.FeedPost{
				CreatedAt: time.Now().Format(bsutil.ISO8601),
				Text:      RandSentence(words, 200),
				Reply: &bsky.FeedPost_ReplyRef{
					Root: &atproto.RepoStrongRef{
						Uri: RandFakeAtUri("app.bsky.feed.post", ""),
						Cid: RandFakeCid().String(),
					},
					Parent: &atproto.RepoStrongRef{
						Uri: RandFakeAtUri("app.bsky.feed.post", ""),
						Cid: RandFakeCid().String(),
					},
				},
			})
			if err != nil {
				return cid.Undef, err
			}
		case "like":
			_, _, err := r.CreateRecord(ctx, "app.bsky.feed.vote", &bsky.FeedLike{
				CreatedAt: time.Now().Format(bsutil.ISO8601),
				Subject: &atproto.RepoStrongRef{
					Uri: RandFakeAtUri("app.bsky.feed.post", ""),
					Cid: RandFakeCid().String(),
				},
			})
			if err != nil {
				return cid.Undef, err
			}
		}

		kmgr := &bsutil.FakeKeyManager{}

		nroot, err := r.Commit(ctx, kmgr.SignForUser)
		if err != nil {
			return cid.Undef, err
		}

		root = nroot
	}

	return root, nil
}
