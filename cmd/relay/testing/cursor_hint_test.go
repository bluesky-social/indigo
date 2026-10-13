package testing

import (
	"context"
	"fmt"
	"os"
	"testing"
	"time"

	comatproto "github.com/bluesky-social/indigo/api/atproto"
	"github.com/bluesky-social/indigo/atproto/identity"
	"github.com/bluesky-social/indigo/atproto/syntax"
	"github.com/bluesky-social/indigo/cmd/relay/stream"

	"github.com/stretchr/testify/assert"
)

// TestCursorHint_NewHost verifies that a cursor hint for a new host
// results in the relay setting LastSeq correctly in the database
func TestCursorHint_NewHost(t *testing.T) {
	assert := assert.New(t)
	ctx := context.Background()

	// Setup test environment
	dir := identity.NewMockDirectory()

	// Load identity from test data to populate mock directory
	scenario, err := LoadScenario(ctx, "testdata/account_lifecycle.json")
	if err != nil {
		t.Fatal(err)
	}
	for _, acc := range scenario.Accounts {
		dir.Insert(acc.Identity)
	}

	tmpd, err := os.MkdirTemp("", "relay-test-cursor-hint-")
	assert.NoError(err)
	defer os.RemoveAll(tmpd)

	// Create a producer (mock PDS) that emits events with sequence numbers
	p := NewProducer()
	hostPort := p.ListenRandom()
	defer p.Shutdown()

	// Create test relay
	sr := MustSimpleRelay(&dir, tmpd, false)

	hostname := fmt.Sprintf("localhost:%d", hostPort)

	// Subscribe to host with cursor hint
	cursorHint := int64(1000)
	err = sr.Relay.SubscribeToHost(ctx, hostname, true, true, cursorHint)
	assert.NoError(err)

	// Verify that LastSeq is set correctly in the database
	// Expected: cursorHint - scrollBack = 1000 - 100 = 900
	host, err := sr.Relay.GetHost(ctx, hostname)
	assert.NoError(err)
	expectedLastSeq := cursorHint - sr.Relay.Config.CursorHintScrollBack
	assert.Equal(expectedLastSeq, host.LastSeq, "LastSeq should be set to cursor hint minus scroll-back buffer")

	// Create consumer connected to relay
	c := NewConsumer(fmt.Sprintf("ws://localhost:%d", sr.Port))
	err = c.Connect(ctx, -1)
	assert.NoError(err)
	defer c.Shutdown()

	// Emit an event after subscription (simulating active PDS)
	// Note: Producer doesn't filter by cursor, so this will be broadcast
	evt := &stream.XRPCStreamEvent{
		RepoIdentity: &comatproto.SyncSubscribeRepos_Identity{
			Did:  "did:plc:vvpy7d6y2li5yo73cgbszibl",
			Seq:  1001,
			Time: syntax.DatetimeNow().String(),
		},
	}
	if err := p.Emit(evt); err != nil {
		t.Fatal(err)
	}

	// Verify event is received
	evts, err := c.ConsumeEvents(1)
	assert.NoError(err)
	assert.Equal(1, len(evts), "should receive event")
	assert.Equal("did:plc:vvpy7d6y2li5yo73cgbszibl", evts[0].RepoIdentity.Did, "should receive event for correct DID")
}

// TestCursorHint_ExistingHost verifies that cursor hint is ignored for existing hosts
func TestCursorHint_ExistingHost(t *testing.T) {
	assert := assert.New(t)
	ctx := context.Background()

	dir := identity.NewMockDirectory()

	// Load identity from test data to populate mock directory
	scenario, err := LoadScenario(ctx, "testdata/account_lifecycle.json")
	if err != nil {
		t.Fatal(err)
	}
	for _, acc := range scenario.Accounts {
		dir.Insert(acc.Identity)
	}

	tmpd, err := os.MkdirTemp("", "relay-test-cursor-hint-existing-")
	assert.NoError(err)
	defer os.RemoveAll(tmpd)

	p := NewProducer()
	hostPort := p.ListenRandom()
	defer p.Shutdown()

	sr := MustSimpleRelay(&dir, tmpd, false)

	hostname := fmt.Sprintf("localhost:%d", hostPort)

	// First subscription: create host without cursor hint (0 = no hint)
	err = sr.Relay.SubscribeToHost(ctx, hostname, true, true, 0)
	assert.NoError(err)

	// Verify initial LastSeq is -1 (no cursor)
	host, err := sr.Relay.GetHost(ctx, hostname)
	assert.NoError(err)
	assert.Equal(int64(-1), host.LastSeq, "initial LastSeq should be -1")

	// Emit an event to potentially establish cursor
	c := NewConsumer(fmt.Sprintf("ws://localhost:%d", sr.Port))
	err = c.Connect(ctx, -1)
	assert.NoError(err)
	defer c.Shutdown()

	evt1 := &stream.XRPCStreamEvent{
		RepoIdentity: &comatproto.SyncSubscribeRepos_Identity{
			Did:  "did:plc:vvpy7d6y2li5yo73cgbszibl",
			Seq:  500,
			Time: syntax.DatetimeNow().String(),
		},
	}
	if err := p.Emit(evt1); err != nil {
		t.Fatal(err)
	}

	// Wait a bit for cursor persistence (happens asynchronously)
	time.Sleep(10 * time.Second)

	// Verify event is received
	_, err = c.ConsumeEvents(1)
	assert.NoError(err)

	// Second subscription: try to use cursor hint (should be ignored)
	// Since the host already exists, cursorHint should be ignored
	cursorHint := int64(1000)
	err = sr.Relay.SubscribeToHost(ctx, hostname, true, true, cursorHint)
	assert.NoError(err)

	host2, err := sr.Relay.GetHost(ctx, hostname)
	assert.NoError(err)
	assert.True(host2.LastSeq == 500, "LastSeq should be updated after cursor persistence")
}

// TestCursorHint_BackwardCompatibility verifies that requestCrawl without
// cursor hint continues to work as before
func TestCursorHint_BackwardCompatibility(t *testing.T) {
	assert := assert.New(t)
	ctx := context.Background()

	dir := identity.NewMockDirectory()

	// Load identity from test data to populate mock directory
	scenario, err := LoadScenario(ctx, "testdata/account_lifecycle.json")
	if err != nil {
		t.Fatal(err)
	}
	for _, acc := range scenario.Accounts {
		dir.Insert(acc.Identity)
	}

	tmpd, err := os.MkdirTemp("", "relay-test-cursor-hint-backward-")
	assert.NoError(err)
	defer os.RemoveAll(tmpd)

	p := NewProducer()
	hostPort := p.ListenRandom()
	defer p.Shutdown()

	sr := MustSimpleRelay(&dir, tmpd, false)

	hostname := fmt.Sprintf("localhost:%d", hostPort)

	// Subscribe without cursor hint (0 = no hint, backward compatible)
	err = sr.Relay.SubscribeToHost(ctx, hostname, true, true, 0)
	assert.NoError(err)

	// Verify LastSeq is -1 (default, no cursor)
	host, err := sr.Relay.GetHost(ctx, hostname)
	assert.NoError(err)
	assert.Equal(int64(-1), host.LastSeq, "LastSeq should be -1 when no cursor hint provided")

	// Emit event after connection (simulating active PDS)
	c := NewConsumer(fmt.Sprintf("ws://localhost:%d", sr.Port))
	err = c.Connect(ctx, -1)
	assert.NoError(err)
	defer c.Shutdown()

	evt := &stream.XRPCStreamEvent{
		RepoIdentity: &comatproto.SyncSubscribeRepos_Identity{
			Did:  "did:plc:vvpy7d6y2li5yo73cgbszibl",
			Seq:  100,
			Time: syntax.DatetimeNow().String(),
		},
	}
	if err := p.Emit(evt); err != nil {
		t.Fatal(err)
	}

	// Verify event is received (same behavior as before)
	evts, err := c.ConsumeEvents(1)
	assert.NoError(err)
	assert.Equal(1, len(evts))
	assert.Equal("did:plc:vvpy7d6y2li5yo73cgbszibl", evts[0].RepoIdentity.Did, "should receive event for correct DID")
}
