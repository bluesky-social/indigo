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

	e1 := evts.Next()
	assert.NotNil(e1.RepoAppend)
	assert.Equal(e1.Repo, bob.DID())

	e2 := evts.Next()
	assert.NotNil(e2.RepoAppend)
	assert.Equal(e2.Repo, alice.DID())

	e3 := evts.Next()
	assert.Equal(e3.Repo, bob.DID())
	assert.Equal(e3.RepoAppend.Ops[0].Kind, "createRecord")

	e4 := evts.Next()
	assert.Equal(e4.Repo, alice.DID())
	assert.Equal(e4.RepoAppend.Ops[0].Kind, "createRecord")

	// playback
	pbevts := b1.Events(t, 2)
	defer pbevts.cancel()

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

func socialSim(t *testing.T, users []*testUser) []*atproto.RepoStrongRef {
	var posts []*atproto.RepoStrongRef
	for i := 0; i < 10; i++ {
		for _, u := range users {
			posts = append(posts, u.Post(t, makeRandomPost()))
		}
	}

	for i := 0; i < 5; i++ {
		for _, u := range users {
			u.Like(t, posts[rand.Intn(len(posts))])
		}
	}

	return posts
}

func TestBGSMultiPDS(t *testing.T) {
	t.Skip()
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
	time.Sleep(time.Millisecond * 50)

	var users []*testUser
	for i := 0; i < 5; i++ {
		users = append(users, p1.MustNewUser(t, usernames[i]+".pdsuno"))
	}

	randomFollows(t, users)
	socialSim(t, users)

	var users2 []*testUser
	for i := 0; i < 5; i++ {
		users2 = append(users2, p2.MustNewUser(t, usernames[i+5]+".pdsdos"))
	}

	randomFollows(t, users2)
	p2posts := socialSim(t, users2)

	randomFollows(t, append(users, users2...))

	users[0].Reply(t, p2posts[0], p2posts[0], "what a wonderful life")

	// now if we make posts on pds 2, the bgs will not hear about those new posts

	p2posts2 := socialSim(t, users2)

	time.Sleep(time.Second)

	p2.RequestScraping(t, b1)
	time.Sleep(time.Millisecond * 50)

	// Now, the bgs will discover a gap, and have to catch up somehow
	socialSim(t, users2)

	// we expect the bgs to learn about posts that it didnt directly see from
	// repos its already partially scraped, as long as its seen *something* after the missing post
	// this is the 'catchup' process
	ctx := context.Background()
	_, err := b1.bgs.Index.GetPost(ctx, p2posts2[4].Uri)
	if err != nil {
		t.Fatal(err)
	}

}
