package server

import (
	"bytes"
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/bluesky-social/indigo/api/atproto"
	"github.com/bluesky-social/indigo/events"
	"github.com/bluesky-social/indigo/internal/cask/models"
	"github.com/bluesky-social/indigo/internal/testutil"
	lexutil "github.com/bluesky-social/indigo/lex/util"
	"github.com/bluesky-social/indigo/pkg/foundation"
	"github.com/bluesky-social/indigo/pkg/prototypes"
	"github.com/google/uuid"
	"github.com/gorilla/websocket"
	"github.com/ipfs/go-cid"
	mh "github.com/multiformats/go-multihash"
	"github.com/stretchr/testify/require"
)

func testDB(t *testing.T) *foundation.DB {
	t.Helper()
	return testutil.TestFoundationDB(t)
}

func testModels(t *testing.T) *models.Models {
	t.Helper()
	db := testDB(t)
	m, err := models.NewWithPrefix(db, uuid.NewString())
	require.NoError(t, err)
	return m
}

// fakeFirehose is a test server that simulates an upstream firehose
type fakeFirehose struct {
	t        *testing.T
	server   *httptest.Server
	upgrader websocket.Upgrader

	mu     sync.Mutex
	events [][]byte // CBOR-encoded events to send
	cursor int64    // cursor received from client
}

func newFakeFirehose(t *testing.T) *fakeFirehose {
	ff := &fakeFirehose{
		t: t,
		upgrader: websocket.Upgrader{
			CheckOrigin: func(r *http.Request) bool { return true },
		},
	}

	ff.server = httptest.NewServer(http.HandlerFunc(ff.handleRequest))
	return ff
}

func (ff *fakeFirehose) URL() string {
	return strings.Replace(ff.server.URL, "http://", "ws://", 1)
}

func (ff *fakeFirehose) Close() {
	ff.server.Close()
}

func (ff *fakeFirehose) handleRequest(w http.ResponseWriter, r *http.Request) {
	// Verify path
	if r.URL.Path != "/xrpc/com.atproto.sync.subscribeRepos" {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}

	// Parse cursor from query params
	if cursorStr := r.URL.Query().Get("cursor"); cursorStr != "" {
		var cursor int64
		if n, err := fmt.Sscanf(cursorStr, "%d", &cursor); err == nil && n == 1 {
			ff.mu.Lock()
			ff.cursor = cursor
			ff.mu.Unlock()
		}
	}

	conn, err := ff.upgrader.Upgrade(w, r, nil)
	if err != nil {
		ff.t.Logf("websocket upgrade failed: %v", err)
		return
	}
	defer conn.Close() //nolint:errcheck

	ff.mu.Lock()
	events := append([][]byte{}, ff.events...) // Copy to avoid holding lock
	ff.mu.Unlock()

	// Send all queued events
	for _, evt := range events {
		if err := conn.WriteMessage(websocket.BinaryMessage, evt); err != nil {
			return
		}
	}

	// Close connection after sending all events - this allows the consumer to
	// receive an error and exit its read loop
	err = conn.WriteMessage(
		websocket.CloseMessage,
		websocket.FormatCloseMessage(websocket.CloseNormalClosure, "done"),
	)
	if err != nil {
		ff.t.Logf("websocket close failed: %v", err)
	}
}

// QueueEvent adds a CBOR-encoded event to be sent to the next client
func (ff *fakeFirehose) QueueEvent(evt []byte) {
	ff.mu.Lock()
	defer ff.mu.Unlock()
	ff.events = append(ff.events, evt)
}

// GetReceivedCursor returns the cursor the client connected with
func (ff *fakeFirehose) GetReceivedCursor() int64 {
	ff.mu.Lock()
	defer ff.mu.Unlock()
	return ff.cursor
}

// makeDummyCID creates a dummy CID for testing
func makeDummyCID(t *testing.T, data string) cid.Cid {
	t.Helper()
	hash, err := mh.Sum([]byte(data), mh.SHA2_256, -1)
	require.NoError(t, err)
	return cid.NewCidV1(cid.DagCBOR, hash)
}

// fakeFirehoseBlocking is a variant that keeps the connection open until closed
type fakeFirehoseBlocking struct {
	t        *testing.T
	server   *httptest.Server
	upgrader websocket.Upgrader

	mu    sync.Mutex
	conns []*websocket.Conn
}

func newFakeFirehoseBlocking(t *testing.T) *fakeFirehoseBlocking {
	ff := &fakeFirehoseBlocking{
		t: t,
		upgrader: websocket.Upgrader{
			CheckOrigin: func(r *http.Request) bool { return true },
		},
	}

	ff.server = httptest.NewServer(http.HandlerFunc(ff.handleRequest))
	return ff
}

func (ff *fakeFirehoseBlocking) URL() string {
	return strings.Replace(ff.server.URL, "http://", "ws://", 1)
}

func (ff *fakeFirehoseBlocking) Close() {
	ff.mu.Lock()
	conns := ff.conns
	ff.mu.Unlock()

	for _, conn := range conns {
		conn.Close() //nolint:errcheck
	}
	ff.server.Close()
}

func (ff *fakeFirehoseBlocking) handleRequest(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/xrpc/com.atproto.sync.subscribeRepos" {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}

	conn, err := ff.upgrader.Upgrade(w, r, nil)
	if err != nil {
		ff.t.Logf("websocket upgrade failed: %v", err)
		return
	}

	ff.mu.Lock()
	ff.conns = append(ff.conns, conn)
	ff.mu.Unlock()

	// Block until connection is closed externally
	for {
		if _, _, err := conn.ReadMessage(); err != nil {
			return
		}
	}
}

// eventOpts holds options for encoding test events
type eventOpts struct {
	seq    int64
	did    string // used for identity, account, sync, labels
	repo   string // used for commit
	active bool   // used for account
}

// encodeEvent creates a CBOR-encoded event of the specified type.
// Supported types: #commit, #identity, #account, #sync, #labels, error, or any custom type.
func encodeEvent(t *testing.T, eventType string, opts eventOpts) []byte {
	t.Helper()

	var buf bytes.Buffer

	// Handle error frame specially (different Op type)
	if eventType == "error" {
		header := events.EventHeader{Op: events.EvtKindErrorFrame}
		err := header.MarshalCBOR(&buf)
		require.NoError(t, err)
		return buf.Bytes()
	}

	// Write header for message types
	header := events.EventHeader{
		Op:      events.EvtKindMessage,
		MsgType: eventType,
	}
	err := header.MarshalCBOR(&buf)
	require.NoError(t, err)

	// Write body based on event type
	now := time.Now().Format(time.RFC3339)
	switch eventType {
	case "#commit":
		repo := opts.repo
		if repo == "" {
			repo = opts.did
		}
		commit := &atproto.SyncSubscribeRepos_Commit{
			Seq:    opts.seq,
			Repo:   repo,
			Rev:    "test-rev",
			Commit: lexutil.LexLink(makeDummyCID(t, repo)),
			Time:   now,
			Blocks: []byte{},
			Ops:    []*atproto.SyncSubscribeRepos_RepoOp{},
		}
		err = commit.MarshalCBOR(&buf)
	case "#identity":
		identity := &atproto.SyncSubscribeRepos_Identity{
			Seq:  opts.seq,
			Did:  opts.did,
			Time: now,
		}
		err = identity.MarshalCBOR(&buf)
	case "#account":
		account := &atproto.SyncSubscribeRepos_Account{
			Seq:    opts.seq,
			Did:    opts.did,
			Time:   now,
			Active: opts.active,
		}
		err = account.MarshalCBOR(&buf)
	case "#sync":
		syncEvt := &atproto.SyncSubscribeRepos_Sync{
			Seq:  opts.seq,
			Did:  opts.did,
			Time: now,
		}
		err = syncEvt.MarshalCBOR(&buf)
	case "#labels":
		labels := &atproto.LabelSubscribeLabels_Labels{
			Seq: opts.seq,
			Labels: []*atproto.LabelDefs_Label{
				{
					Src: opts.did,
					Uri: "at://" + opts.did + "/app.bsky.feed.post/test",
					Val: "test-label",
					Cts: now,
				},
			},
		}
		err = labels.MarshalCBOR(&buf)
	default:
		// Unknown event type - write minimal CBOR body (empty map)
		buf.Write([]byte{0xa0})
		return buf.Bytes()
	}
	require.NoError(t, err)

	return buf.Bytes()
}

func TestConsumer_BasicEvents(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	m := testModels(t)
	ff := newFakeFirehose(t)
	defer ff.Close()

	// Queue some events
	ff.QueueEvent(encodeEvent(t, "#commit", eventOpts{seq: 100, repo: "did:plc:test1"}))
	ff.QueueEvent(encodeEvent(t, "#commit", eventOpts{seq: 101, repo: "did:plc:test2"}))
	ff.QueueEvent(encodeEvent(t, "#identity", eventOpts{seq: 102, did: "did:plc:test3"}))

	// Create and run consumer
	consumer := newFirehoseConsumer(slog.Default(), m, ff.URL())

	// Run consumer in background
	consumerCtx, consumerCancel := context.WithCancel(ctx)
	consumerDone := make(chan error, 1)
	go func() {
		consumerDone <- consumer.Run(consumerCtx)
	}()

	// Wait for consumer to finish (it will exit when fake firehose closes connection)
	select {
	case <-consumerDone:
		// Consumer exited (expected - fake firehose closes after sending events)
	case <-time.After(5 * time.Second):
		consumerCancel()
		t.Fatal("consumer did not finish in time")
	}
	consumerCancel() // Clean up context

	// Verify events were written correctly
	events, _, err := m.GetEventsSince(ctx, nil, 10)
	require.NoError(t, err)
	require.Len(t, events, 3)

	// Events should be in order
	require.Equal(t, int64(100), events[0].UpstreamSeq)
	require.Equal(t, "#commit", events[0].EventType)

	require.Equal(t, int64(101), events[1].UpstreamSeq)
	require.Equal(t, "#commit", events[1].EventType)

	require.Equal(t, int64(102), events[2].UpstreamSeq)
	require.Equal(t, "#identity", events[2].EventType)
}

func TestConsumer_ResumesFromCursor(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	m := testModels(t)

	// Pre-populate some events to simulate a previous run
	require.NoError(t, m.WriteEvent(ctx, &prototypes.FirehoseEvent{
		UpstreamSeq: 50,
		EventType:   "#commit",
		RawEvent:    encodeEvent(t, "#commit", eventOpts{seq: 50, repo: "did:plc:old"}),
	}))

	// Create fake firehose that will track the cursor
	ff := newFakeFirehose(t)
	defer ff.Close()

	// Queue new events (that would come after cursor=50)
	ff.QueueEvent(encodeEvent(t, "#commit", eventOpts{seq: 51, repo: "did:plc:new1"}))
	ff.QueueEvent(encodeEvent(t, "#commit", eventOpts{seq: 52, repo: "did:plc:new2"}))

	// Create and run consumer
	consumer := newFirehoseConsumer(slog.Default(), m, ff.URL())

	consumerCtx, consumerCancel := context.WithCancel(ctx)
	consumerDone := make(chan error, 1)
	go func() {
		consumerDone <- consumer.Run(consumerCtx)
	}()

	// Wait for consumer to finish
	select {
	case <-consumerDone:
	case <-time.After(5 * time.Second):
		consumerCancel()
		t.Fatal("consumer did not finish in time")
	}
	consumerCancel()

	// Verify the consumer connected with the correct cursor
	require.Equal(t, int64(50), ff.GetReceivedCursor(), "consumer should resume from cursor=50")

	// Verify all events are in FDB
	events, _, err := m.GetEventsSince(ctx, nil, 10)
	require.NoError(t, err)
	require.Len(t, events, 3) // 1 pre-existing + 2 new
}

func TestConsumer_EmptyDatabase(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	m := testModels(t)
	ff := newFakeFirehose(t)
	defer ff.Close()

	ff.QueueEvent(encodeEvent(t, "#commit", eventOpts{seq: 1, repo: "did:plc:first"}))

	consumer := newFirehoseConsumer(slog.Default(), m, ff.URL())

	consumerCtx, consumerCancel := context.WithCancel(ctx)
	consumerDone := make(chan error, 1)
	go func() {
		consumerDone <- consumer.Run(consumerCtx)
	}()

	// Wait for consumer to finish
	select {
	case <-consumerDone:
	case <-time.After(5 * time.Second):
		consumerCancel()
		t.Fatal("consumer did not finish in time")
	}
	consumerCancel()

	// Should not have sent a cursor (cursor=0 means no cursor param)
	require.Equal(t, int64(0), ff.GetReceivedCursor(), "should not send cursor when database is empty")

	// Verify event was written
	seq, err := m.GetLatestUpstreamSeq(ctx)
	require.NoError(t, err)
	require.Equal(t, int64(1), seq)
}

func TestConsumer_AllEventTypes(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	m := testModels(t)
	ff := newFakeFirehose(t)
	defer ff.Close()

	// Queue all supported event types
	ff.QueueEvent(encodeEvent(t, "#commit", eventOpts{seq: 1, repo: "did:plc:user1"}))
	ff.QueueEvent(encodeEvent(t, "#identity", eventOpts{seq: 2, did: "did:plc:user2"}))
	ff.QueueEvent(encodeEvent(t, "#account", eventOpts{seq: 3, did: "did:plc:user3", active: true}))
	ff.QueueEvent(encodeEvent(t, "#sync", eventOpts{seq: 4, did: "did:plc:user4"}))
	ff.QueueEvent(encodeEvent(t, "#labels", eventOpts{seq: 5, did: "did:plc:labeler"}))
	ff.QueueEvent(encodeEvent(t, "error", eventOpts{}))

	consumer := newFirehoseConsumer(slog.Default(), m, ff.URL())

	consumerCtx, consumerCancel := context.WithCancel(ctx)
	consumerDone := make(chan error, 1)
	go func() {
		consumerDone <- consumer.Run(consumerCtx)
	}()

	select {
	case <-consumerDone:
	case <-time.After(5 * time.Second):
		consumerCancel()
		t.Fatal("consumer did not finish in time")
	}
	consumerCancel()

	evts, _, err := m.GetEventsSince(ctx, nil, 10)
	require.NoError(t, err)
	require.Len(t, evts, 6)

	// Verify all event types were stored correctly
	require.Equal(t, "#commit", evts[0].EventType)
	require.Equal(t, int64(1), evts[0].UpstreamSeq)

	require.Equal(t, "#identity", evts[1].EventType)
	require.Equal(t, int64(2), evts[1].UpstreamSeq)

	require.Equal(t, "#account", evts[2].EventType)
	require.Equal(t, int64(3), evts[2].UpstreamSeq)

	require.Equal(t, "#sync", evts[3].EventType)
	require.Equal(t, int64(4), evts[3].UpstreamSeq)

	require.Equal(t, "#labels", evts[4].EventType)
	require.Equal(t, int64(5), evts[4].UpstreamSeq)

	require.Equal(t, "error", evts[5].EventType)
	require.Equal(t, int64(0), evts[5].UpstreamSeq) // error frames have no sequence
}

func TestConsumer_ContextCancellation(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	m := testModels(t)

	// Create a fake firehose that doesn't close immediately
	ff := newFakeFirehoseBlocking(t)
	defer ff.Close()

	consumer := newFirehoseConsumer(slog.Default(), m, ff.URL())

	consumerCtx, consumerCancel := context.WithCancel(ctx)
	consumerDone := make(chan error, 1)
	go func() {
		consumerDone <- consumer.Run(consumerCtx)
	}()

	// Give consumer time to connect
	time.Sleep(100 * time.Millisecond)

	// Cancel context and close the connection to trigger shutdown.
	// In production, the connection would be closed by the ping handler
	// after detecting the context is done or by setting read deadlines.
	consumerCancel()
	ff.Close()

	// Consumer should exit within a reasonable time
	select {
	case err := <-consumerDone:
		// Consumer should exit cleanly or with a websocket close error
		if err != nil {
			require.Contains(t, err.Error(), "websocket")
		}
	case <-time.After(20 * time.Second):
		t.Fatal("consumer did not shut down in time after context cancellation")
	}
}

func TestConsumer_LargeEventBatch(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	m := testModels(t)
	ff := newFakeFirehose(t)
	defer ff.Close()

	// Queue a large batch of events
	const numEvents = 100
	for i := range numEvents {
		ff.QueueEvent(encodeEvent(t, "#commit", eventOpts{seq: int64(i + 1), repo: fmt.Sprintf("did:plc:user%d", i)}))
	}

	consumer := newFirehoseConsumer(slog.Default(), m, ff.URL())

	consumerCtx, consumerCancel := context.WithCancel(ctx)
	consumerDone := make(chan error, 1)
	go func() {
		consumerDone <- consumer.Run(consumerCtx)
	}()

	select {
	case <-consumerDone:
	case <-time.After(20 * time.Second):
		consumerCancel()
		t.Fatal("consumer did not finish in time")
	}
	consumerCancel()

	// Verify all events were written
	evts, _, err := m.GetEventsSince(ctx, nil, numEvents+10)
	require.NoError(t, err)
	require.Len(t, evts, numEvents)

	// Verify ordering - events should be in write order
	for i, evt := range evts {
		require.Equal(t, int64(i+1), evt.UpstreamSeq)
	}
}

func TestConsumer_UnknownMessageType(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	m := testModels(t)
	ff := newFakeFirehose(t)
	defer ff.Close()

	// Create an event with an unknown message type
	ff.QueueEvent(encodeEvent(t, "#unknown_future_type", eventOpts{}))
	ff.QueueEvent(encodeEvent(t, "#commit", eventOpts{seq: 100, repo: "did:plc:test"}))

	consumer := newFirehoseConsumer(slog.Default(), m, ff.URL())

	consumerCtx, consumerCancel := context.WithCancel(ctx)
	consumerDone := make(chan error, 1)
	go func() {
		consumerDone <- consumer.Run(consumerCtx)
	}()

	select {
	case <-consumerDone:
	case <-time.After(5 * time.Second):
		consumerCancel()
		t.Fatal("consumer did not finish in time")
	}
	consumerCancel()

	evts, _, err := m.GetEventsSince(ctx, nil, 10)
	require.NoError(t, err)
	require.Len(t, evts, 2)

	// Unknown event should still be stored, but with seq=0
	require.Equal(t, "#unknown_future_type", evts[0].EventType)
	require.Equal(t, int64(0), evts[0].UpstreamSeq)

	// Normal event should follow
	require.Equal(t, "#commit", evts[1].EventType)
	require.Equal(t, int64(100), evts[1].UpstreamSeq)
}
