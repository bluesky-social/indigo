package testing

import (
	"bytes"
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"encoding/base32"
	"encoding/json"
	"fmt"
	mathrand "math/rand"
	"net"
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
	"github.com/bluesky-social/indigo/events/schedulers/sequential"
	"github.com/bluesky-social/indigo/indexer"
	lexutil "github.com/bluesky-social/indigo/lex/util"
	"github.com/bluesky-social/indigo/models"
	"github.com/bluesky-social/indigo/notifs"
	"github.com/bluesky-social/indigo/pds"
	"github.com/bluesky-social/indigo/plc"
	"github.com/bluesky-social/indigo/repo"
	"github.com/bluesky-social/indigo/repomgr"
	bsutil "github.com/bluesky-social/indigo/util"
	"github.com/bluesky-social/indigo/xrpc"
	"github.com/ipfs/go-cid"
	"github.com/multiformats/go-multihash"
	"github.com/whyrusleeping/go-did"

	"net/url"

	"github.com/gorilla/websocket"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

type TestPDS struct {
	dir    string
	server *pds.Server
	plc    *api.PLCServer

	listener net.Listener

	shutdown func()
}

// HTTPHost returns a host:port string that the PDS server is running at
func (tp *TestPDS) RawHost() string {
	return tp.listener.Addr().String()
}

// HTTPHost returns a URL string that the PDS server is running at with the
// scheme set for HTTP
func (tp *TestPDS) HTTPHost() string {
	u := url.URL{Scheme: "http", Host: tp.listener.Addr().String()}
	return u.String()
}

func (tp *TestPDS) Cleanup() {
	if tp.shutdown != nil {
		tp.shutdown()
	}

	if tp.dir != "" {
		_ = os.RemoveAll(tp.dir)
	}
}

func MustSetupPDS(t *testing.T, suffix string, plc plc.PLCClient) *TestPDS {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	tpds, err := SetupPDS(ctx, suffix, plc)
	if err != nil {
		t.Fatal(err)
	}

	return tpds
}

func SetupPDS(ctx context.Context, suffix string, plc plc.PLCClient) (*TestPDS, error) {
	dir, err := os.MkdirTemp("", "integtest")
	if err != nil {
		return nil, err
	}

	maindb, err := gorm.Open(sqlite.Open(filepath.Join(dir, "test.sqlite?cache=shared&mode=rwc")))
	if err != nil {
		return nil, err
	}

	tx := maindb.Exec("PRAGMA journal_mode=WAL;")
	if tx.Error != nil {
		return nil, tx.Error
	}

	cardb, err := gorm.Open(sqlite.Open(filepath.Join(dir, "car.sqlite")))
	if err != nil {
		return nil, err
	}

	cspath := filepath.Join(dir, "carstore")
	if err := os.Mkdir(cspath, 0775); err != nil {
		return nil, err
	}

	cs, err := carstore.NewCarStore(cardb, []string{cspath})
	if err != nil {
		return nil, err
	}

	raw, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return nil, fmt.Errorf("failed to generate new ECDSA private key: %s", err)
	}
	serkey := &did.PrivKey{
		Raw:  raw,
		Type: did.KeyTypeP256,
	}

	var lc net.ListenConfig
	li, err := lc.Listen(ctx, "tcp", "localhost:0")
	if err != nil {
		return nil, err
	}

	host := li.Addr().String()
	srv, err := pds.NewServer(maindb, cs, serkey, suffix, host, plc, []byte(host+suffix))
	if err != nil {
		return nil, err
	}

	return &TestPDS{
		dir:      dir,
		server:   srv,
		listener: li,
	}, nil
}

func (tp *TestPDS) Run(t *testing.T) {
	// TODO: rig this up so it t.Fatals if the RunAPI call fails immediately
	go func() {
		if err := tp.server.RunAPIWithListener(tp.listener); err != nil {
			fmt.Println(err)
		}
	}()
	time.Sleep(time.Millisecond * 10)

	tp.shutdown = func() {
		tp.server.Shutdown(context.TODO())
	}
}

func (tp *TestPDS) RequestScraping(t *testing.T, b *TestRelay) {
	t.Helper()

	err := b.bgs.CreateAdminToken("test")
	if err != nil {
		t.Fatal(err)
	}

	req, err := http.NewRequest("POST", "http://"+b.Host()+"/admin/subs/setPerDayLimit?limit=500", nil)

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer test")

	// Send the request
	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	// Check the response
	if resp.StatusCode != http.StatusOK {
		t.Fatal("expected 200 OK, got: ", resp.Status)
	}

	c := &xrpc.Client{Host: "http://" + b.Host()}
	if err := atproto.SyncRequestCrawl(context.TODO(), c, &atproto.SyncRequestCrawl_Input{Hostname: tp.RawHost()}); err != nil {
		t.Fatal(err)
	}
}

func (tp *TestPDS) BumpLimits(t *testing.T, b *TestRelay) {
	t.Helper()

	err := b.bgs.CreateAdminToken("test")
	if err != nil {
		t.Fatal(err)
	}

	u, err := url.Parse(tp.HTTPHost())
	if err != nil {
		t.Fatal(err)
	}

	limReqBody := bgs.RateLimitChangeRequest{
		Host:      u.Host,
		PerSecond: 5_000,
		PerHour:   100_000,
		PerDay:    1_000_000,
		RepoLimit: 500_000,
		CrawlRate: 50_000,
	}

	// JSON encode the request body
	reqBody, err := json.Marshal(limReqBody)
	if err != nil {
		t.Fatal(err)
	}

	req, err := http.NewRequest("POST", "http://"+b.Host()+"/admin/pds/changeLimits", bytes.NewBuffer(reqBody))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer test")

	// Send the request
	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	// Check the response
	if resp.StatusCode != http.StatusOK {
		t.Fatal("expected 200 OK, got: ", resp.Status)
	}
}

type TestUser struct {
	handle string
	pds    *TestPDS
	did    string

	client *xrpc.Client
}

func (tp *TestPDS) MustNewUser(t *testing.T, handle string) *TestUser {
	t.Helper()

	u, err := tp.NewUser(handle)
	if err != nil {
		t.Fatal(err)
	}

	return u
}

func (tp *TestPDS) NewUser(handle string) (*TestUser, error) {
	ctx := context.TODO()

	c := &xrpc.Client{
		Host: tp.HTTPHost(),
	}

	fmt.Println("HOST: ", c.Host)
	email := handle + "@fake.com"
	pass := "password"
	out, err := atproto.ServerCreateAccount(ctx, c, &atproto.ServerCreateAccount_Input{
		Email:    &email,
		Handle:   handle,
		Password: &pass,
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

	return &TestUser{
		pds:    tp,
		handle: out.Handle,
		client: c,
		did:    out.Did,
	}, nil
}

func (tp *TestPDS) TakedownRepo(t *testing.T, did string) {
	req, err := http.NewRequest("GET", tp.HTTPHost()+"/takedownRepo?did="+did, nil)
	if err != nil {
		t.Fatal(err)
	}

	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		t.Fatal(err)
	}

	if resp.StatusCode != http.StatusOK {
		t.Fatal("expected 200 OK, got: ", resp.Status)
	}
}

func (tp *TestPDS) SuspendRepo(t *testing.T, did string) {
	req, err := http.NewRequest("GET", tp.HTTPHost()+"/suspendRepo?did="+did, nil)
	if err != nil {
		t.Fatal(err)
	}

	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		t.Fatal(err)
	}

	if resp.StatusCode != http.StatusOK {
		t.Fatal("expected 200 OK, got: ", resp.Status)
	}
}

func (tp *TestPDS) DeactivateRepo(t *testing.T, did string) {
	req, err := http.NewRequest("GET", tp.HTTPHost()+"/deactivateRepo?did="+did, nil)
	if err != nil {
		t.Fatal(err)
	}

	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		t.Fatal(err)
	}

	if resp.StatusCode != http.StatusOK {
		t.Fatal("expected 200 OK, got: ", resp.Status)
	}
}

func (tp *TestPDS) ReactivateRepo(t *testing.T, did string) {
	req, err := http.NewRequest("GET", tp.HTTPHost()+"/reactivateRepo?did="+did, nil)
	if err != nil {
		t.Fatal(err)
	}

	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		t.Fatal(err)
	}

	if resp.StatusCode != http.StatusOK {
		t.Fatal("expected 200 OK, got: ", resp.Status)
	}
}

func (u *TestUser) Reply(t *testing.T, replyto, root *atproto.RepoStrongRef, body string) string {
	t.Helper()

	ctx := context.TODO()
	resp, err := atproto.RepoCreateRecord(ctx, u.client, &atproto.RepoCreateRecord_Input{
		Collection: "app.bsky.feed.post",
		Repo:       u.did,
		Record: &lexutil.LexiconTypeDecoder{Val: &bsky.FeedPost{
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

func (u *TestUser) DID() string {
	return u.did
}

func (u *TestUser) Post(t *testing.T, body string) *atproto.RepoStrongRef {
	t.Helper()

	ctx := context.TODO()
	resp, err := atproto.RepoCreateRecord(ctx, u.client, &atproto.RepoCreateRecord_Input{
		Collection: "app.bsky.feed.post",
		Repo:       u.did,
		Record: &lexutil.LexiconTypeDecoder{Val: &bsky.FeedPost{
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

func (u *TestUser) Like(t *testing.T, post *atproto.RepoStrongRef) {
	t.Helper()

	ctx := context.TODO()
	_, err := atproto.RepoCreateRecord(ctx, u.client, &atproto.RepoCreateRecord_Input{
		Collection: "app.bsky.feed.vote",
		Repo:       u.did,
		Record: &lexutil.LexiconTypeDecoder{Val: &bsky.FeedLike{
			LexiconTypeID: "app.bsky.feed.vote",
			CreatedAt:     time.Now().Format(time.RFC3339),
			Subject:       post,
		}},
	})
	if err != nil {
		t.Fatal(err)
	}

}

func (u *TestUser) Follow(t *testing.T, did string) string {
	t.Helper()

	ctx := context.TODO()
	resp, err := atproto.RepoCreateRecord(ctx, u.client, &atproto.RepoCreateRecord_Input{
		Collection: "app.bsky.graph.follow",
		Repo:       u.did,
		Record: &lexutil.LexiconTypeDecoder{Val: &bsky.GraphFollow{
			CreatedAt: time.Now().Format(time.RFC3339),
			Subject:   did,
		}},
	})

	if err != nil {
		t.Fatal(err)
	}

	return resp.Uri
}

func (u *TestUser) GetFeed(t *testing.T) []*bsky.FeedDefs_FeedViewPost {
	t.Helper()

	ctx := context.TODO()
	resp, err := bsky.FeedGetTimeline(ctx, u.client, "reverse-chronlogical", "", 100)
	if err != nil {
		t.Fatal(err)
	}

	return resp.Feed
}

func (u *TestUser) GetNotifs(t *testing.T) []*bsky.NotificationListNotifications_Notification {
	t.Helper()

	ctx := context.TODO()
	resp, err := bsky.NotificationListNotifications(ctx, u.client, "", 100, false, "")
	if err != nil {
		t.Fatal(err)
	}

	return resp.Notifications
}

func (u *TestUser) ChangeHandle(t *testing.T, nhandle string) {
	t.Helper()

	ctx := context.TODO()
	if err := atproto.IdentityUpdateHandle(ctx, u.client, &atproto.IdentityUpdateHandle_Input{
		Handle: nhandle,
	}); err != nil {
		t.Fatal(err)
	}
}

func TestPLC(t *testing.T) *plc.FakeDid {
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

type TestRelay struct {
	bgs *bgs.BGS
	tr  *api.TestHandleResolver
	db  *gorm.DB

	// listener is owned by by the Relay structure and should be closed by
	// shutting down the Relay.
	listener net.Listener
}

func (t *TestRelay) Host() string {
	return t.listener.Addr().String()
}

func MustSetupRelay(t *testing.T, didr plc.PLCClient) *TestRelay {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	tbgs, err := SetupRelay(ctx, didr)
	if err != nil {
		t.Fatal(err)
	}

	return tbgs
}

func SetupRelay(ctx context.Context, didr plc.PLCClient) (*TestRelay, error) {
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

	/*
		cs, err := carstore.NewCarStore(cardb, []string{cspath})
		if err != nil {
			return nil, err
		}
		//*/
	//*
	cs, err := carstore.NewNonArchivalCarstore(cardb)
	if err != nil {
		return nil, err
	}
	//*/

	//kmgr := indexer.NewKeyManager(didr, nil)
	kmgr := &bsutil.FakeKeyManager{}

	repoman := repomgr.NewRepoManager(cs, kmgr)

	notifman := notifs.NewNotificationManager(maindb, repoman.GetRecord)

	opts := events.DefaultDiskPersistOptions()
	opts.EventsPerFile = 10
	diskpersist, err := events.NewDiskPersistence(filepath.Join(dir, "dp-primary"), filepath.Join(dir, "dp-archive"), maindb, opts)

	evtman := events.NewEventManager(diskpersist)
	rf := indexer.NewRepoFetcher(maindb, repoman, 10)

	ix, err := indexer.NewIndexer(maindb, notifman, evtman, didr, rf, true, true, true)
	if err != nil {
		return nil, err
	}

	repoman.SetEventHandler(func(ctx context.Context, evt *repomgr.RepoEvent) {
		if err := ix.HandleRepoEvent(ctx, evt); err != nil {
			fmt.Println("test relay failed to handle repo event", err)
		}
	}, true) // TODO: actually want this to be false, but some tests use this to confirm the Relay has seen certain records

	tr := &api.TestHandleResolver{}

	bgsConfig := bgs.DefaultBGSConfig()
	bgsConfig.SSL = false
	b, err := bgs.NewBGS(maindb, ix, repoman, evtman, didr, rf, tr, bgsConfig)
	if err != nil {
		return nil, err
	}

	var lc net.ListenConfig
	listener, err := lc.Listen(ctx, "tcp", "localhost:0")
	if err != nil {
		return nil, err
	}

	return &TestRelay{
		db:       maindb,
		bgs:      b,
		tr:       tr,
		listener: listener,
	}, nil
}

func (b *TestRelay) Run(t *testing.T) {
	go func() {
		if err := b.bgs.StartWithListener(b.listener); err != nil {
			fmt.Println(err)
		}
	}()
	time.Sleep(time.Millisecond * 10)
}

func (b *TestRelay) BanDomain(t *testing.T, d string) {
	t.Helper()

	if err := b.db.Create(&models.DomainBan{
		Domain: d,
	}).Error; err != nil {
		t.Fatal(err)
	}
}

type EventStream struct {
	Lk     sync.Mutex
	Events []*events.XRPCStreamEvent
	Cancel func()

	Cur int
}

func (b *TestRelay) Events(t *testing.T, since int64) *EventStream {
	d := websocket.Dialer{}
	h := http.Header{}

	q := ""
	if since >= 0 {
		q = fmt.Sprintf("?cursor=%d", since)
	}

	con, resp, err := d.Dial("ws://"+b.Host()+"/xrpc/com.atproto.sync.subscribeRepos"+q, h)
	if err != nil {
		t.Fatal(err)
	}

	if resp.StatusCode != 101 {
		t.Fatal("expected http 101 response, got: ", resp.StatusCode)
	}

	ctx, cancel := context.WithCancel(context.Background())

	es := &EventStream{
		Cancel: cancel,
	}

	go func() {
		<-ctx.Done()
		con.Close()
	}()

	go func() {
		rsc := &events.RepoStreamCallbacks{
			RepoCommit: func(evt *atproto.SyncSubscribeRepos_Commit) error {
				fmt.Println("received event: ", evt.Seq, evt.Repo, len(es.Events))
				es.Lk.Lock()
				es.Events = append(es.Events, &events.XRPCStreamEvent{RepoCommit: evt})
				es.Lk.Unlock()
				return nil
			},
			RepoHandle: func(evt *atproto.SyncSubscribeRepos_Handle) error {
				fmt.Println("received handle event: ", evt.Seq, evt.Did)
				es.Lk.Lock()
				es.Events = append(es.Events, &events.XRPCStreamEvent{RepoHandle: evt})
				es.Lk.Unlock()
				return nil
			},
			RepoIdentity: func(evt *atproto.SyncSubscribeRepos_Identity) error {
				fmt.Println("received identity event: ", evt.Seq, evt.Did)
				es.Lk.Lock()
				es.Events = append(es.Events, &events.XRPCStreamEvent{RepoIdentity: evt})
				es.Lk.Unlock()
				return nil
			},
			RepoAccount: func(evt *atproto.SyncSubscribeRepos_Account) error {
				fmt.Println("received account event: ", evt.Seq, evt.Did)
				es.Lk.Lock()
				es.Events = append(es.Events, &events.XRPCStreamEvent{RepoAccount: evt})
				es.Lk.Unlock()
				return nil
			},
		}
		seqScheduler := sequential.NewScheduler("test", rsc.EventHandler)
		if err := events.HandleRepoStream(ctx, con, seqScheduler); err != nil {
			fmt.Println(err)
		}
	}()

	return es
}

func (es *EventStream) Next() *events.XRPCStreamEvent {
	defer es.Lk.Unlock()
	for {
		es.Lk.Lock()
		if len(es.Events) > es.Cur {
			es.Cur++
			return es.Events[es.Cur-1]
		}
		es.Lk.Unlock()
		time.Sleep(time.Millisecond * 10)
	}
}

func (es *EventStream) All() []*events.XRPCStreamEvent {
	es.Lk.Lock()
	defer es.Lk.Unlock()
	out := make([]*events.XRPCStreamEvent, len(es.Events))
	for i, e := range es.Events {
		out[i] = e
	}

	return out
}

func (es *EventStream) WaitFor(n int) []*events.XRPCStreamEvent {
	var out []*events.XRPCStreamEvent
	for i := 0; i < n; i++ {
		out = append(out, es.Next())
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

func MakeRandomPost() string {
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

func RandAction() string {
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
		switch RandAction() {
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

		nroot, _, err := r.Commit(ctx, kmgr.SignForUser)
		if err != nil {
			return cid.Undef, err
		}

		root = nroot
	}

	return root, nil
}
