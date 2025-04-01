package testing

import (
	"context"
	"testing"

	comatproto "github.com/bluesky-social/indigo/api/atproto"
	"github.com/bluesky-social/indigo/atproto/syntax"
	"github.com/bluesky-social/indigo/events"

	"github.com/stretchr/testify/assert"
)

// meta test for the testing framework itself. simply connects the consumer to the producer
func TestFramework(t *testing.T) {
	assert := assert.New(t)
	ctx := context.Background() // XXX

	p := NewProducer(":9900")
	p.Listen()
	defer p.Shutdown()

	c := NewConsumer("ws://localhost:9900")
	err := c.Connect(ctx, -1)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Shutdown()

	h := "example.atbin.dev"
	e1 := events.XRPCStreamEvent{
		RepoIdentity: &comatproto.SyncSubscribeRepos_Identity{
			Did:    "did:web:example.atbin.dev",
			Handle: &h,
			Seq:    1234,
			Time:   syntax.DatetimeNow().String(),
		},
	}
	p.Emit(&e1)

	evts, err := c.ConsumeEvents(1)
	if err != nil {
		t.Fatal(err)
	}
	assert.Equal(1, len(evts))
	assert.Equal(e1.RepoIdentity, evts[0].RepoIdentity)
}
