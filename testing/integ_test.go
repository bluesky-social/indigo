package testing

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"math/rand"
	"strings"
	"testing"
	"time"

	atproto "github.com/bluesky-social/indigo/api/atproto"
	"github.com/bluesky-social/indigo/repo"
	"github.com/bluesky-social/indigo/xrpc"
	"github.com/ipfs/go-cid"
	"github.com/ipfs/go-log/v2"
	car "github.com/ipld/go-car"
	"github.com/stretchr/testify/assert"
)

func init() {
	log.SetAllLoggers(log.LevelInfo)
}

func TestBGSBasic(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping BGS test in 'short' test mode")
	}
	assert := assert.New(t)
	didr := TestPLC(t)
	p1 := MustSetupPDS(t, ".tpds", didr)
	p1.Run(t)

	b1 := MustSetupBGS(t, didr)
	b1.Run(t)

	b1.tr.TrialHosts = []string{p1.RawHost()}

	p1.RequestScraping(t, b1)

	time.Sleep(time.Millisecond * 50)

	evts := b1.Events(t, -1)
	defer evts.Cancel()

	bob := p1.MustNewUser(t, "bob.tpds")
	fmt.Println("event 1")
	e1 := evts.Next()
	assert.NotNil(e1.RepoCommit)
	assert.Equal(e1.RepoCommit.Repo, bob.DID())

	alice := p1.MustNewUser(t, "alice.tpds")
	fmt.Println("event 2")
	e2 := evts.Next()
	assert.NotNil(e2.RepoCommit)
	assert.Equal(e2.RepoCommit.Repo, alice.DID())

	bp1 := bob.Post(t, "cats for cats")
	ap1 := alice.Post(t, "no i like dogs")

	_ = bp1
	_ = ap1

	fmt.Println("bob:", bob.DID())
	fmt.Println("event 3")
	e3 := evts.Next()
	assert.Equal(e3.RepoCommit.Repo, bob.DID())
	//assert.Equal(e3.RepoCommit.Ops[0].Kind, "createRecord")

	fmt.Println("alice:", alice.DID())
	fmt.Println("event 4")
	e4 := evts.Next()
	assert.Equal(e4.RepoCommit.Repo, alice.DID())
	//assert.Equal(e4.RepoCommit.Ops[0].Kind, "createRecord")

	// playback
	pbevts := b1.Events(t, 2)
	defer pbevts.Cancel()

	fmt.Println("event 5")
	pbe1 := pbevts.Next()
	assert.Equal(*e3, *pbe1)
}

func randomFollows(t *testing.T, users []*TestUser) {
	for n := 0; n < 3; n++ {
		for i, u := range users {
			oi := rand.Intn(len(users))
			if i == oi {
				continue
			}

			u.Follow(t, users[oi].DID())
		}
	}
}

func socialSim(t *testing.T, users []*TestUser, postiter, likeiter int) []*atproto.RepoStrongRef {
	var posts []*atproto.RepoStrongRef
	for i := 0; i < postiter; i++ {
		for _, u := range users {
			posts = append(posts, u.Post(t, MakeRandomPost()))
		}
	}

	for i := 0; i < likeiter; i++ {
		for _, u := range users {
			u.Like(t, posts[rand.Intn(len(posts))])
		}
	}

	return posts
}

func TestBGSMultiPDS(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping BGS test in 'short' test mode")
	}
	//t.Skip("test too sleepy to run in CI for now")

	assert := assert.New(t)
	_ = assert
	didr := TestPLC(t)
	p1 := MustSetupPDS(t, ".pdsuno", didr)
	p1.Run(t)

	p2 := MustSetupPDS(t, ".pdsdos", didr)
	p2.Run(t)

	b1 := MustSetupBGS(t, didr)
	b1.Run(t)

	b1.tr.TrialHosts = []string{p1.RawHost(), p2.RawHost()}

	p1.RequestScraping(t, b1)
	time.Sleep(time.Millisecond * 100)

	var users []*TestUser
	for i := 0; i < 5; i++ {
		users = append(users, p1.MustNewUser(t, usernames[i]+".pdsuno"))
	}

	randomFollows(t, users)
	socialSim(t, users, 10, 10)

	var users2 []*TestUser
	for i := 0; i < 5; i++ {
		users2 = append(users2, p2.MustNewUser(t, usernames[i+5]+".pdsdos"))
	}

	randomFollows(t, users2)
	p2posts := socialSim(t, users2, 10, 10)

	randomFollows(t, append(users, users2...))

	users[0].Reply(t, p2posts[0], p2posts[0], "what a wonderful life")

	// now if we make posts on pds 2, the bgs will not hear about those new posts

	p2posts2 := socialSim(t, users2, 10, 10)

	time.Sleep(time.Second)

	p2.RequestScraping(t, b1)
	time.Sleep(time.Millisecond * 50)

	// Now, the bgs will discover a gap, and have to catch up somehow
	socialSim(t, users2, 1, 0)

	time.Sleep(time.Second)

	// we expect the bgs to learn about posts that it didnt directly see from
	// repos its already partially scraped, as long as its seen *something* after the missing post
	// this is the 'catchup' process
	ctx := context.Background()
	_, err := b1.bgs.Index.GetPost(ctx, p2posts2[4].Uri)
	if err != nil {
		t.Fatal(err)
	}
}

func TestBGSMultiGap(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping BGS test in 'short' test mode")
	}
	//t.Skip("test too sleepy to run in CI for now")
	assert := assert.New(t)
	_ = assert
	didr := TestPLC(t)
	p1 := MustSetupPDS(t, ".pdsuno", didr)
	p1.Run(t)

	p2 := MustSetupPDS(t, ".pdsdos", didr)
	p2.Run(t)

	b1 := MustSetupBGS(t, didr)
	b1.Run(t)

	b1.tr.TrialHosts = []string{p1.RawHost(), p2.RawHost()}

	p1.RequestScraping(t, b1)
	time.Sleep(time.Millisecond * 50)

	users := []*TestUser{p1.MustNewUser(t, usernames[0]+".pdsuno")}

	socialSim(t, users, 10, 0)

	users2 := []*TestUser{p2.MustNewUser(t, usernames[1]+".pdsdos")}

	p2posts := socialSim(t, users2, 10, 0)

	users[0].Reply(t, p2posts[0], p2posts[0], "what a wonderful life")
	time.Sleep(time.Millisecond * 100)

	ctx := context.Background()
	_, err := b1.bgs.Index.GetPost(ctx, p2posts[3].Uri)
	if err != nil {
		t.Fatal(err)
	}

	// now if we make posts on pds 2, the bgs will not hear about those new posts

	p2posts2 := socialSim(t, users2, 10, 0)

	time.Sleep(time.Second)

	p2.RequestScraping(t, b1)
	time.Sleep(time.Millisecond * 50)

	// Now, the bgs will discover a gap, and have to catch up somehow
	socialSim(t, users2, 1, 0)

	time.Sleep(time.Second)

	// we expect the bgs to learn about posts that it didnt directly see from
	// repos its already partially scraped, as long as its seen *something* after the missing post
	// this is the 'catchup' process
	_, err = b1.bgs.Index.GetPost(ctx, p2posts2[4].Uri)
	if err != nil {
		t.Fatal(err)
	}
}

func TestHandleChange(t *testing.T) {
	//t.Skip("test too sleepy to run in CI for now")
	assert := assert.New(t)
	_ = assert
	didr := TestPLC(t)
	p1 := MustSetupPDS(t, ".pdsuno", didr)
	p1.Run(t)

	b1 := MustSetupBGS(t, didr)
	b1.Run(t)

	b1.tr.TrialHosts = []string{p1.RawHost()}

	p1.RequestScraping(t, b1)
	time.Sleep(time.Millisecond * 50)

	evts := b1.Events(t, -1)

	u := p1.MustNewUser(t, usernames[0]+".pdsuno")

	// if the handle changes before the bgs processes the first event, things
	// get a little weird
	time.Sleep(time.Millisecond * 50)
	//socialSim(t, []*testUser{u}, 10, 0)

	u.ChangeHandle(t, "catbear.pdsuno")

	time.Sleep(time.Millisecond * 100)

	initevt := evts.Next()
	fmt.Println(initevt.RepoCommit)
	hcevt := evts.Next()
	fmt.Println(hcevt.RepoHandle)
}

func TestBGSTakedown(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping BGS test in 'short' test mode")
	}
	assert := assert.New(t)
	_ = assert

	didr := TestPLC(t)
	p1 := MustSetupPDS(t, ".tpds", didr)
	p1.Run(t)

	b1 := MustSetupBGS(t, didr)
	b1.Run(t)

	b1.tr.TrialHosts = []string{p1.RawHost()}

	p1.RequestScraping(t, b1)

	time.Sleep(time.Millisecond * 50)
	es1 := b1.Events(t, 0)

	bob := p1.MustNewUser(t, "bob.tpds")
	alice := p1.MustNewUser(t, "alice.tpds")

	bob.Post(t, "cats for cats")
	alice.Post(t, "no i like dogs")
	bp2 := bob.Post(t, "im a bad person who deserves to be taken down")
	bob.Like(t, bp2)

	expCount := 6
	evts1 := es1.WaitFor(expCount)
	assert.Equal(expCount, len(evts1))

	assert.NoError(b1.bgs.TakeDownRepo(context.TODO(), bob.did))

	es2 := b1.Events(t, 0)
	time.Sleep(time.Millisecond * 50) // wait for events to stream in and be collected
	evts2 := es2.WaitFor(2)

	assert.Equal(2, len(evts2))
	for _, e := range evts2 {
		if e.RepoCommit.Repo == bob.did {
			t.Fatal("events from bob were not removed")
		}
	}

	bob.Post(t, "im gonna sneak through being banned")
	time.Sleep(time.Millisecond * 50)
	alice.Post(t, "im a normal person")
	// ensure events from bob dont get through

	last := es2.Next()
	assert.Equal(alice.did, last.RepoCommit.Repo)
}

func TestRebase(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping BGS test in 'short' test mode")
	}
	assert := assert.New(t)
	didr := TestPLC(t)
	p1 := MustSetupPDS(t, ".tpds", didr)
	p1.Run(t)

	b1 := MustSetupBGS(t, didr)
	b1.Run(t)

	b1.tr.TrialHosts = []string{p1.RawHost()}

	p1.RequestScraping(t, b1)

	time.Sleep(time.Millisecond * 50)

	bob := p1.MustNewUser(t, "bob.tpds")

	bob.Post(t, "cats for cats")
	bob.Post(t, "i am the king of the world")
	bob.Post(t, "the name is bob")
	bob.Post(t, "why cant i eat pie")

	time.Sleep(time.Millisecond * 100)

	evts1 := b1.Events(t, 0)
	defer evts1.Cancel()

	preRebaseEvts := evts1.WaitFor(5)
	fmt.Println(preRebaseEvts)

	bob.DoRebase(t)

	rbevt := evts1.Next()
	assert.Equal(true, rbevt.RepoCommit.Rebase)

	sc := commitFromSlice(t, rbevt.RepoCommit.Blocks, (cid.Cid)(rbevt.RepoCommit.Commit))
	assert.Nil(sc.Prev)

	lev := preRebaseEvts[4]
	oldsc := commitFromSlice(t, lev.RepoCommit.Blocks, (cid.Cid)(lev.RepoCommit.Commit))

	assert.Equal(sc.Data, oldsc.Data)

	evts2 := b1.Events(t, 0)
	afterEvts := evts2.WaitFor(1)
	assert.Equal(true, afterEvts[0].RepoCommit.Rebase)
}

func TestRebaseMulti(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping BGS test in 'short' test mode")
	}
	assert := assert.New(t)
	didr := TestPLC(t)
	p1 := MustSetupPDS(t, ".tpds", didr)
	p1.Run(t)

	b1 := MustSetupBGS(t, didr)
	b1.Run(t)

	b1.tr.TrialHosts = []string{p1.RawHost()}

	p1.RequestScraping(t, b1)

	esgenesis := b1.Events(t, 0)

	time.Sleep(time.Millisecond * 50)

	bob := p1.MustNewUser(t, "bob.tpds")

	for i := 0; i < 10; i++ {
		bob.Post(t, fmt.Sprintf("this is bobs post %d", i))
	}

	// wait for 11 events, the first one is the actor creation
	firsten := esgenesis.WaitFor(11)
	_ = firsten

	fmt.Println("REBASE ONE")
	bob.DoRebase(t)

	var posts []*atproto.RepoStrongRef
	for i := 0; i < 10; i++ {
		ref := bob.Post(t, fmt.Sprintf("this is bobs post after rebase %d", i))
		posts = append(posts, ref)
	}

	time.Sleep(time.Millisecond * 50)

	evts1 := b1.Events(t, 0)
	defer evts1.Cancel()

	all := evts1.WaitFor(11)

	assert.Equal(true, all[0].RepoCommit.Rebase)
	assert.Equal(int64(12), all[0].RepoCommit.Seq)
	assert.Equal(posts[0].Cid, all[1].RepoCommit.Ops[0].Cid.String())

	// and another one!
	fmt.Println("REBASE TWO")
	bob.DoRebase(t)

	var posts2 []*atproto.RepoStrongRef
	for i := 0; i < 15; i++ {
		ref := bob.Post(t, fmt.Sprintf("this is bobs post after second rebase %d", i))
		posts2 = append(posts2, ref)
	}

	time.Sleep(time.Millisecond * 50)

	evts2 := b1.Events(t, 0)
	defer evts2.Cancel()

	all = evts2.WaitFor(16)

	assert.Equal(true, all[0].RepoCommit.Rebase)
	assert.Equal(posts2[0].Cid, all[1].RepoCommit.Ops[0].Cid.String())
}

func jsonPrint(v any) {
	b, _ := json.Marshal(v)
	fmt.Println(string(b))
}

func commitFromSlice(t *testing.T, slice []byte, rcid cid.Cid) *repo.SignedCommit {
	carr, err := car.NewCarReader(bytes.NewReader(slice))
	if err != nil {
		t.Fatal(err)
	}

	for {
		blk, err := carr.Next()
		if err != nil {
			t.Fatal(err)
		}

		if blk.Cid() == rcid {

			var sc repo.SignedCommit
			if err := sc.UnmarshalCBOR(bytes.NewReader(blk.RawData())); err != nil {
				t.Fatal(err)
			}
			return &sc
		}
	}
}

func TestDomainBans(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping BGS test in 'short' test mode")
	}
	didr := TestPLC(t)

	b1 := MustSetupBGS(t, didr)
	b1.Run(t)

	b1.BanDomain(t, "foo.com")

	c := &xrpc.Client{Host: "http://" + b1.Host()}
	if err := atproto.SyncRequestCrawl(context.TODO(), c, "foo.com"); err == nil {
		t.Fatal("domain should be banned")
	}

	if err := atproto.SyncRequestCrawl(context.TODO(), c, "pds.foo.com"); err == nil {
		t.Fatal("domain should be banned")
	}

	if err := atproto.SyncRequestCrawl(context.TODO(), c, "app.pds.foo.com"); err == nil {
		t.Fatal("domain should be banned")
	}

	// should not be banned
	err := atproto.SyncRequestCrawl(context.TODO(), c, "foo.bar.com")
	if err == nil {
		t.Fatal("should still fail")
	}

	if !strings.Contains(err.Error(), "XRPC ERROR 401") {
		t.Fatal("should have failed with a 401")
	}

}
