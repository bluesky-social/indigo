package testing

import (
	"fmt"
	"testing"
	"time"

	"github.com/ipfs/go-log/v2"
	"github.com/stretchr/testify/assert"
)

func init() {
	log.SetAllLoggers(log.LevelDebug)
}

func TestBGSBasic(t *testing.T) {
	assert := assert.New(t)
	didr := testPLC(t)
	p1 := setupPDS(t, "localhost:5155", ".tpds", didr)
	p1.Run(t)

	b1 := setupBGS(t, "localhost:8231", didr)
	b1.Run(t)

	p1.RequestScraping(t, b1)

	time.Sleep(time.Millisecond * 50)

	evts := b1.Events(t, -1)
	defer evts.cancel()

	bob := p1.NewUser(t, "bob.tpds")
	alice := p1.NewUser(t, "alice.tpds")

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
