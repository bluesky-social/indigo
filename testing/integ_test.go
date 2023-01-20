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

	evts := b1.Events(t)
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
	assert.Equal(e1.Kind, "repoChange")
	assert.Equal(e1.Repo, bob.DID())

	e2 := evts.Next()
	e3 := evts.Next()
	e4 := evts.Next()

	_ = e2
	_ = e3
	_ = e4
}
