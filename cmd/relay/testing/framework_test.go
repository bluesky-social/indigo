package testing

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"testing"

	comatproto "github.com/gander-social/gander-indigo-sovereign/api/atproto"
	"github.com/gander-social/gander-indigo-sovereign/atproto/syntax"
	"github.com/gander-social/gander-indigo-sovereign/cmd/relay/stream"

	"github.com/stretchr/testify/assert"
)

// meta test for the testing framework itself. simply connects the consumer to the producer
func TestFramework(t *testing.T) {
	assert := assert.New(t)
	ctx := context.Background()

	p := NewProducer()
	port := p.ListenRandom()
	defer p.Shutdown()

	c := NewConsumer(fmt.Sprintf("ws://localhost:%d", port))
	err := c.Connect(ctx, -1)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Shutdown()

	h := "example.atbin.dev"
	e1 := stream.XRPCStreamEvent{
		RepoIdentity: &comatproto.SyncSubscribeRepos_Identity{
			Did:    "did:web:example.atbin.dev",
			Handle: &h,
			Seq:    1234,
			Time:   syntax.DatetimeNow().String(),
		},
	}
	if err := p.Emit(&e1); err != nil {
		t.Fatal(err)
	}

	evts, err := c.ConsumeEvents(1)
	if err != nil {
		t.Fatal(err)
	}
	assert.Equal(1, len(evts))
	assert.Equal(e1.RepoIdentity, evts[0].RepoIdentity)
}

// simply loads a scenario from JSON and checks data looks right
func TestScenarioLoad(t *testing.T) {
	assert := assert.New(t)

	fixBytes, err := os.ReadFile("testdata/legacy.json")
	if err != nil {
		t.Fatal(err)
	}

	var s Scenario
	if err = json.Unmarshal(fixBytes, &s); err != nil {
		t.Fatal(err)
	}
	assert.Equal(1, len(s.Accounts))
	assert.Equal("active", s.Accounts[0].Status)
	assert.Equal("https://morel.us-east.host.gndr.network", s.Accounts[0].Identity.PDSEndpoint())
	_, err = s.Accounts[0].Identity.PublicKey()
	assert.NoError(err)
	assert.Equal(3, len(s.Messages))
	msg, err := s.Messages[2].Frame.XRPCStreamEvent()
	if err != nil {
		t.Fatal(err)
	}
	assert.Equal(int64(7278969010), msg.RepoCommit.Seq)
	assert.Equal(4945, len(msg.RepoCommit.Blocks))
	assert.Equal(1, len(msg.RepoCommit.Ops))
}

func TestBasicScenario(t *testing.T) {
	ctx := context.Background()

	err := LoadAndRunScenario(ctx, "testdata/legacy.json")
	if err != nil {
		t.Fatal(err)
	}
}
