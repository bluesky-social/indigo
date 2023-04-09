package testing

import (
	"context"
	"fmt"
	"math/rand"
	"testing"
	"time"

	atproto "github.com/bluesky-social/indigo/api/atproto"
	"github.com/ipfs/go-log/v2"
	"github.com/stretchr/testify/assert"
)

func init() {
	log.SetAllLoggers(log.LevelDebug)
}

func TestBGSBasic(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping BGS test in 'short' test mode")
	}
	assert := assert.New(t)
	didr := testPLC(t)
	p1 := mustSetupPDS(t, "localhost:5155", ".tpds", didr)
	p1.Run(t)

	b1 := mustSetupBGS(t, "localhost:8231", didr)
	b1.Run(t)

	p1.RequestScraping(t, b1)

	time.Sleep(time.Millisecond * 50)

	evts := b1.Events(t, -1)
	defer evts.cancel()

	bob := p1.MustNewUser(t, "bob.tpds")
	alice := p1.MustNewUser(t, "alice.tpds")

	bp1 := bob.Post(t, "cats for cats")
	ap1 := alice.Post(t, "no i like dogs")

	_ = bp1
	_ = ap1

	fmt.Println("bob:", bob.DID())
	fmt.Println("alice:", alice.DID())

	fmt.Println("event 1")
	e1 := evts.Next()
	assert.NotNil(e1.RepoCommit)
	assert.Equal(e1.RepoCommit.Repo, bob.DID())

	fmt.Println("event 2")
	e2 := evts.Next()
	assert.NotNil(e2.RepoCommit)
	assert.Equal(e2.RepoCommit.Repo, alice.DID())

	fmt.Println("event 3")
	e3 := evts.Next()
	assert.Equal(e3.RepoCommit.Repo, bob.DID())
	//assert.Equal(e3.RepoCommit.Ops[0].Kind, "createRecord")

	fmt.Println("event 4")
	e4 := evts.Next()
	assert.Equal(e4.RepoCommit.Repo, alice.DID())
	//assert.Equal(e4.RepoCommit.Ops[0].Kind, "createRecord")

	// playback
	pbevts := b1.Events(t, 2)
	defer pbevts.cancel()

	fmt.Println("event 5")
	pbe1 := pbevts.Next()
	assert.Equal(*e3, *pbe1)
}

func randomFollows(t *testing.T, users []*testUser) {
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

func socialSim(t *testing.T, users []*testUser, postiter, likeiter int) []*atproto.RepoStrongRef {
	var posts []*atproto.RepoStrongRef
	for i := 0; i < postiter; i++ {
		for _, u := range users {
			posts = append(posts, u.Post(t, makeRandomPost()))
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
	didr := testPLC(t)
	p1 := mustSetupPDS(t, "localhost:5185", ".pdsuno", didr)
	p1.Run(t)

	p2 := mustSetupPDS(t, "localhost:5186", ".pdsdos", didr)
	p2.Run(t)

	b1 := mustSetupBGS(t, "localhost:8281", didr)
	b1.Run(t)

	p1.RequestScraping(t, b1)
	time.Sleep(time.Millisecond * 100)

	var users []*testUser
	for i := 0; i < 5; i++ {
		users = append(users, p1.MustNewUser(t, usernames[i]+".pdsuno"))
	}

	randomFollows(t, users)
	socialSim(t, users, 10, 10)

	var users2 []*testUser
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
	didr := testPLC(t)
	p1 := mustSetupPDS(t, "localhost:5195", ".pdsuno", didr)
	p1.Run(t)

	p2 := mustSetupPDS(t, "localhost:5196", ".pdsdos", didr)
	p2.Run(t)

	b1 := mustSetupBGS(t, "localhost:8291", didr)
	b1.Run(t)

	p1.RequestScraping(t, b1)
	time.Sleep(time.Millisecond * 50)

	users := []*testUser{p1.MustNewUser(t, usernames[0]+".pdsuno")}

	socialSim(t, users, 10, 0)

	users2 := []*testUser{p2.MustNewUser(t, usernames[1]+".pdsdos")}

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
	didr := testPLC(t)
	p1 := mustSetupPDS(t, "localhost:5195", ".pdsuno", didr)
	p1.Run(t)

	b1 := mustSetupBGS(t, "localhost:8291", didr)
	b1.Run(t)

	p1.RequestScraping(t, b1)
	time.Sleep(time.Millisecond * 50)

	evts := b1.Events(t, -1)

	u := p1.MustNewUser(t, usernames[0]+".pdsuno")

	//socialSim(t, []*testUser{u}, 10, 0)

	u.ChangeHandle(t, "catbear.pdsuno")

	time.Sleep(time.Millisecond * 100)

	initevt := evts.Next()
	fmt.Println(initevt.RepoCommit)
	hcevt := evts.Next()
	fmt.Println(hcevt.RepoHandle)
}
