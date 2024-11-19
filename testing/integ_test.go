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
	"github.com/bluesky-social/indigo/events"
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

func TestRelayBasic(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping Relay test in 'short' test mode")
	}
	assert := assert.New(t)
	didr := TestPLC(t)
	p1 := MustSetupPDS(t, ".tpds", didr)
	p1.Run(t)

	b1 := MustSetupRelay(t, didr)
	b1.Run(t)

	b1.tr.TrialHosts = []string{p1.RawHost()}

	p1.RequestScraping(t, b1)
	p1.BumpLimits(t, b1)

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

func TestRelayMultiPDS(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping Relay test in 'short' test mode")
	}
	//t.Skip("test too sleepy to run in CI for now")

	assert := assert.New(t)
	_ = assert
	didr := TestPLC(t)
	p1 := MustSetupPDS(t, ".pdsuno", didr)
	p1.Run(t)

	p2 := MustSetupPDS(t, ".pdsdos", didr)
	p2.Run(t)

	b1 := MustSetupRelay(t, didr)
	b1.Run(t)

	b1.tr.TrialHosts = []string{p1.RawHost(), p2.RawHost()}

	p1.RequestScraping(t, b1)
	p1.BumpLimits(t, b1)
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

	// now if we make posts on pds 2, the relay will not hear about those new posts

	p2posts2 := socialSim(t, users2, 10, 10)

	time.Sleep(time.Second)

	p2.RequestScraping(t, b1)
	p2.BumpLimits(t, b1)
	time.Sleep(time.Millisecond * 50)

	// Now, the relay will discover a gap, and have to catch up somehow
	socialSim(t, users2, 1, 0)

	time.Sleep(time.Second)

	// we expect the relay to learn about posts that it did not directly see from
	// repos its already partially scraped, as long as its seen *something* after the missing post
	// this is the 'catchup' process
	ctx := context.Background()
	_, err := b1.bgs.Index.GetPost(ctx, p2posts2[4].Uri)
	if err != nil {
		t.Fatal(err)
	}
}

func TestRelayMultiGap(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping Relay test in 'short' test mode")
	}
	//t.Skip("test too sleepy to run in CI for now")
	assert := assert.New(t)
	_ = assert
	didr := TestPLC(t)
	p1 := MustSetupPDS(t, ".pdsuno", didr)
	p1.Run(t)

	p2 := MustSetupPDS(t, ".pdsdos", didr)
	p2.Run(t)

	b1 := MustSetupRelay(t, didr)
	b1.Run(t)

	b1.tr.TrialHosts = []string{p1.RawHost(), p2.RawHost()}

	p1.RequestScraping(t, b1)
	p1.BumpLimits(t, b1)
	time.Sleep(time.Millisecond * 250)

	users := []*TestUser{p1.MustNewUser(t, usernames[0]+".pdsuno")}

	socialSim(t, users, 10, 0)

	users2 := []*TestUser{p2.MustNewUser(t, usernames[1]+".pdsdos")}

	p2posts := socialSim(t, users2, 10, 0)

	users[0].Reply(t, p2posts[0], p2posts[0], "what a wonderful life")
	time.Sleep(time.Second * 2)

	ctx := context.Background()
	_, err := b1.bgs.Index.GetPost(ctx, p2posts[3].Uri)
	if err != nil {
		t.Fatal(err)
	}

	// now if we make posts on pds 2, the relay will not hear about those new posts

	p2posts2 := socialSim(t, users2, 10, 0)

	time.Sleep(time.Second)

	p2.RequestScraping(t, b1)
	p2.BumpLimits(t, b1)
	time.Sleep(time.Second * 2)

	// Now, the relay will discover a gap, and have to catch up somehow
	socialSim(t, users2, 1, 0)

	time.Sleep(time.Second * 2)

	// we expect the relay to learn about posts that it did not directly see from
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

	b1 := MustSetupRelay(t, didr)
	b1.Run(t)

	b1.tr.TrialHosts = []string{p1.RawHost()}

	p1.RequestScraping(t, b1)
	p1.BumpLimits(t, b1)
	time.Sleep(time.Millisecond * 50)

	evts := b1.Events(t, -1)

	u := p1.MustNewUser(t, usernames[0]+".pdsuno")

	// if the handle changes before the relay processes the first event, things
	// get a little weird
	time.Sleep(time.Millisecond * 50)
	//socialSim(t, []*testUser{u}, 10, 0)

	u.ChangeHandle(t, "catbear.pdsuno")

	time.Sleep(time.Millisecond * 100)

	initevt := evts.Next()
	fmt.Println(initevt.RepoCommit)
	hcevt := evts.Next()
	fmt.Println(hcevt.RepoHandle)
	idevt := evts.Next()
	fmt.Println(idevt.RepoIdentity)
}

func TestAccountEvent(t *testing.T) {
	assert := assert.New(t)
	_ = assert
	didr := TestPLC(t)
	p1 := MustSetupPDS(t, ".pdsuno", didr)
	p1.Run(t)

	b1 := MustSetupRelay(t, didr)
	b1.Run(t)

	b1.tr.TrialHosts = []string{p1.RawHost()}

	p1.RequestScraping(t, b1)
	p1.BumpLimits(t, b1)
	time.Sleep(time.Millisecond * 50)

	evts := b1.Events(t, -1)

	u := p1.MustNewUser(t, usernames[0]+".pdsuno")

	// if the handle changes before the relay processes the first event, things
	// get a little weird
	time.Sleep(time.Millisecond * 50)
	//socialSim(t, []*testUser{u}, 10, 0)

	p1.TakedownRepo(t, u.DID())
	p1.ReactivateRepo(t, u.DID())
	p1.DeactivateRepo(t, u.DID())
	p1.ReactivateRepo(t, u.DID())
	p1.SuspendRepo(t, u.DID())
	p1.ReactivateRepo(t, u.DID())

	time.Sleep(time.Millisecond * 100)

	initevt := evts.Next()
	fmt.Println(initevt.RepoCommit)

	// Takedown
	acevt := evts.Next()
	fmt.Println(acevt.RepoAccount)
	assert.Equal(acevt.RepoAccount.Did, u.DID())
	assert.Equal(acevt.RepoAccount.Active, false)
	assert.Equal(*acevt.RepoAccount.Status, events.AccountStatusTakendown)

	// Reactivate
	acevt = evts.Next()
	fmt.Println(acevt.RepoAccount)
	assert.Equal(acevt.RepoAccount.Did, u.DID())
	assert.Equal(acevt.RepoAccount.Active, true)
	assert.Equal(*acevt.RepoAccount.Status, events.AccountStatusActive)

	// Deactivate
	acevt = evts.Next()
	fmt.Println(acevt.RepoAccount)
	assert.Equal(acevt.RepoAccount.Did, u.DID())
	assert.Equal(acevt.RepoAccount.Active, false)
	assert.Equal(*acevt.RepoAccount.Status, events.AccountStatusDeactivated)

	// Reactivate
	acevt = evts.Next()
	fmt.Println(acevt.RepoAccount)
	assert.Equal(acevt.RepoAccount.Did, u.DID())
	assert.Equal(acevt.RepoAccount.Active, true)
	assert.Equal(*acevt.RepoAccount.Status, events.AccountStatusActive)

	// Suspend
	acevt = evts.Next()
	fmt.Println(acevt.RepoAccount)
	assert.Equal(acevt.RepoAccount.Did, u.DID())
	assert.Equal(acevt.RepoAccount.Active, false)
	assert.Equal(*acevt.RepoAccount.Status, events.AccountStatusSuspended)

	// Reactivate
	acevt = evts.Next()
	fmt.Println(acevt.RepoAccount)
	assert.Equal(acevt.RepoAccount.Did, u.DID())
	assert.Equal(acevt.RepoAccount.Active, true)
	assert.Equal(*acevt.RepoAccount.Status, events.AccountStatusActive)

	// Takedown at Relay level, then emit active event and make sure relay overrides it
	b1.bgs.TakeDownRepo(context.TODO(), u.DID())
	p1.ReactivateRepo(t, u.DID())

	time.Sleep(time.Millisecond * 20)

	acevt = evts.Next()
	fmt.Println(acevt.RepoAccount)
	assert.Equal(acevt.RepoAccount.Did, u.DID())
	assert.Equal(acevt.RepoAccount.Active, false)
	assert.Equal(*acevt.RepoAccount.Status, events.AccountStatusTakendown)

	// Reactivate at Relay level, then emit an active account event and make sure relay passes it through
	b1.bgs.ReverseTakedown(context.TODO(), u.DID())
	p1.ReactivateRepo(t, u.DID())

	time.Sleep(time.Millisecond * 20)

	acevt = evts.Next()
	fmt.Println(acevt.RepoAccount)
	assert.Equal(acevt.RepoAccount.Did, u.DID())
	assert.Equal(acevt.RepoAccount.Active, true)
	assert.Equal(*acevt.RepoAccount.Status, events.AccountStatusActive)
}

func TestRelayTakedown(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping Relay test in 'short' test mode")
	}
	assert := assert.New(t)
	_ = assert

	didr := TestPLC(t)
	p1 := MustSetupPDS(t, ".tpds", didr)
	p1.Run(t)

	b1 := MustSetupRelay(t, didr)
	b1.Run(t)

	b1.tr.TrialHosts = []string{p1.RawHost()}

	p1.RequestScraping(t, b1)
	p1.BumpLimits(t, b1)

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
		t.Skip("skipping Relay test in 'short' test mode")
	}
	didr := TestPLC(t)

	b1 := MustSetupRelay(t, didr)
	b1.Run(t)

	b1.BanDomain(t, "foo.com")

	c := &xrpc.Client{Host: "http://" + b1.Host()}
	if err := atproto.SyncRequestCrawl(context.TODO(), c, &atproto.SyncRequestCrawl_Input{Hostname: "foo.com"}); err == nil {
		t.Fatal("domain should be banned")
	}

	if err := atproto.SyncRequestCrawl(context.TODO(), c, &atproto.SyncRequestCrawl_Input{Hostname: "pds.foo.com"}); err == nil {
		t.Fatal("domain should be banned")
	}

	err := atproto.SyncRequestCrawl(context.TODO(), c, &atproto.SyncRequestCrawl_Input{Hostname: "app.pds.foo.com"})
	if err == nil {
		t.Fatal("domain should be banned")
	}

	if !strings.Contains(err.Error(), "XRPC ERROR 401") {
		t.Fatal("should have failed with a 401")
	}

	// should not be banned
	err = atproto.SyncRequestCrawl(context.TODO(), c, &atproto.SyncRequestCrawl_Input{Hostname: "foo.bar.com"})
	if err == nil {
		t.Fatal("should still fail")
	}

	if !strings.Contains(err.Error(), "XRPC ERROR 400") {
		t.Fatal("should have failed with a 400")
	}
}

func TestRelayHandleEmptyEvent(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping Relay test in 'short' test mode")
	}
	assert := assert.New(t)
	didr := TestPLC(t)
	p1 := MustSetupPDS(t, ".tpds", didr)
	p1.Run(t)

	b1 := MustSetupRelay(t, didr)
	b1.Run(t)

	b1.tr.TrialHosts = []string{p1.RawHost()}

	p1.RequestScraping(t, b1)
	p1.BumpLimits(t, b1)

	time.Sleep(time.Millisecond * 50)

	evts := b1.Events(t, -1)
	defer evts.Cancel()

	bob := p1.MustNewUser(t, "bob.tpds")
	fmt.Println("event 1")
	e1 := evts.Next()
	assert.NotNil(e1.RepoCommit)
	assert.Equal(e1.RepoCommit.Repo, bob.DID())

	ctx := context.TODO()
	rm := p1.server.Repoman()
	if err := rm.BatchWrite(ctx, 1, nil); err != nil {
		t.Fatal(err)
	}

	e2 := evts.Next()
	fmt.Println(e2.RepoCommit.Ops)
	assert.Equal(len(e2.RepoCommit.Ops), 0)
	assert.Equal(e2.RepoCommit.Repo, bob.DID())
}
