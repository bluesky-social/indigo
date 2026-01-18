package server

import (
	"context"
	"log/slog"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/bluesky-social/indigo/internal/cask/models"
	"github.com/bluesky-social/indigo/internal/testutil"
	"github.com/bluesky-social/indigo/pkg/foundation"
	"github.com/bluesky-social/indigo/pkg/prototypes"
	"github.com/google/uuid"
	"github.com/gorilla/websocket"
	"github.com/labstack/echo/v4"
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

func testServer(t *testing.T) (*Server, *models.Models) {
	t.Helper()
	m := testModels(t)
	s := &Server{
		log:           slog.Default(),
		models:        m,
		subscribersMu: &sync.Mutex{},
	}
	return s, m
}

// startTestServer creates an httptest server with the subscribeRepos endpoint
func startTestServer(t *testing.T, s *Server) *httptest.Server {
	t.Helper()
	e := echo.New()
	e.GET("/xrpc/com.atproto.sync.subscribeRepos", s.handleSubscribeRepos)
	return httptest.NewServer(e)
}

// dialWebSocket connects to the test server's subscribeRepos endpoint
func dialWebSocket(t *testing.T, server *httptest.Server, cursor string) *websocket.Conn {
	t.Helper()
	url := strings.Replace(server.URL, "http://", "ws://", 1) + "/xrpc/com.atproto.sync.subscribeRepos"
	if cursor != "" {
		url += "?cursor=" + cursor
	}
	conn, _, err := websocket.DefaultDialer.Dial(url, nil)
	require.NoError(t, err)
	return conn
}

// readMessage reads a message with a timeout, handling errors appropriately for tests
func readMessage(t *testing.T, conn *websocket.Conn, timeout time.Duration) (int, []byte, error) {
	t.Helper()
	_ = conn.SetReadDeadline(time.Now().Add(timeout)) //nolint:errcheck
	return conn.ReadMessage()
}

func TestSubscribeRepos_ReceivesEvents(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	s, m := testServer(t)
	server := startTestServer(t, s)
	defer server.Close()

	// Write some events to FDB
	const numEvents = 20
	for i := range numEvents {
		event := &prototypes.FirehoseEvent{
			UpstreamSeq: int64(100 + i),
			EventType:   "#commit",
			RawEvent:    []byte("event-" + string(rune('A'+i))),
		}
		err := m.WriteEvent(ctx, event)
		require.NoError(t, err)
	}

	// Connect via WebSocket with cursor=0 to receive all historical events
	conn := dialWebSocket(t, server, "0")
	defer conn.Close() //nolint:errcheck

	// Should receive the same number of events
	for i := range numEvents {
		msgType, data, err := readMessage(t, conn, 5*time.Second)
		require.NoError(t, err)
		require.Equal(t, websocket.BinaryMessage, msgType)
		require.Equal(t, []byte("event-"+string(rune('A'+i))), data)
	}
}

func TestSubscribeRepos_WithCursor(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	s, m := testServer(t)
	server := startTestServer(t, s)
	defer server.Close()

	// Write events with seqs 100, 101, 102, 103, 104
	for i := range 5 {
		event := &prototypes.FirehoseEvent{
			UpstreamSeq: int64(100 + i),
			EventType:   "#commit",
			RawEvent:    []byte("event-" + string(rune('A'+i))),
		}
		err := m.WriteEvent(ctx, event)
		require.NoError(t, err)
	}

	// Connect with cursor=101 (should receive 102, 103, 104)
	conn := dialWebSocket(t, server, "101")
	defer conn.Close() //nolint:errcheck

	// Should receive events after cursor
	expectedEvents := []string{"event-C", "event-D", "event-E"}
	for _, expected := range expectedEvents {
		msgType, data, err := readMessage(t, conn, 5*time.Second)
		require.NoError(t, err)
		require.Equal(t, websocket.BinaryMessage, msgType)
		require.Equal(t, []byte(expected), data)
	}
}

func TestSubscribeRepos_CursorWithGaps(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	s, m := testServer(t)
	server := startTestServer(t, s)
	defer server.Close()

	// Write events with gaps: 1000, 1005, 1010
	seqs := []int64{1000, 1005, 1010}
	for i, seq := range seqs {
		event := &prototypes.FirehoseEvent{
			UpstreamSeq: seq,
			EventType:   "#commit",
			RawEvent:    []byte("event-" + string(rune('A'+i))),
		}
		err := m.WriteEvent(ctx, event)
		require.NoError(t, err)
	}

	// Connect with cursor=1002 (doesn't exist, floors to 1000)
	// Should receive events after 1000: 1005, 1010
	conn := dialWebSocket(t, server, "1002")
	defer conn.Close() //nolint:errcheck

	expectedEvents := []string{"event-B", "event-C"}
	for _, expected := range expectedEvents {
		msgType, data, err := readMessage(t, conn, 5*time.Second)
		require.NoError(t, err)
		require.Equal(t, websocket.BinaryMessage, msgType)
		require.Equal(t, []byte(expected), data)
	}
}

func TestSubscribeRepos_NoCursorStartsFromTip(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	s, m := testServer(t)
	server := startTestServer(t, s)
	defer server.Close()

	// Write an event BEFORE connecting
	event := &prototypes.FirehoseEvent{
		UpstreamSeq: 100,
		EventType:   "#commit",
		RawEvent:    []byte("old-event"),
	}
	err := m.WriteEvent(ctx, event)
	require.NoError(t, err)

	// Connect WITHOUT a cursor - should start from the "tip" and not receive the old event
	conn := dialWebSocket(t, server, "")
	defer conn.Close() //nolint:errcheck

	// Write a new event AFTER connecting
	event2 := &prototypes.FirehoseEvent{
		UpstreamSeq: 101,
		EventType:   "#commit",
		RawEvent:    []byte("new-event"),
	}
	err = m.WriteEvent(ctx, event2)
	require.NoError(t, err)

	// Should receive only the new event (not the old one)
	msgType, data, err := readMessage(t, conn, 5*time.Second)
	require.NoError(t, err)
	require.Equal(t, websocket.BinaryMessage, msgType)
	require.Equal(t, []byte("new-event"), data)
}

func TestSubscribeRepos_StreamsNewEvents(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	s, m := testServer(t)
	server := startTestServer(t, s)
	defer server.Close()

	// Write all events before connecting
	event := &prototypes.FirehoseEvent{
		UpstreamSeq: 100,
		EventType:   "#commit",
		RawEvent:    []byte("initial"),
	}
	err := m.WriteEvent(ctx, event)
	require.NoError(t, err)

	for i := range 3 {
		event := &prototypes.FirehoseEvent{
			UpstreamSeq: int64(101 + i),
			EventType:   "#commit",
			RawEvent:    []byte("streamed-" + string(rune('A'+i))),
		}
		err := m.WriteEvent(ctx, event)
		require.NoError(t, err)
	}

	// Connect with cursor=0 to receive all historical events
	conn := dialWebSocket(t, server, "0")
	defer conn.Close() //nolint:errcheck

	// Receive initial event
	_, data, err := readMessage(t, conn, 5*time.Second)
	require.NoError(t, err)
	require.Equal(t, []byte("initial"), data)

	// Receive the streamed events
	for i := range 3 {
		_, data, err := readMessage(t, conn, 5*time.Second)
		require.NoError(t, err)
		require.Equal(t, []byte("streamed-"+string(rune('A'+i))), data)
	}
}

func TestSubscribeRepos_MultipleSubscribers(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	s, m := testServer(t)
	server := startTestServer(t, s)
	defer server.Close()

	// Write an event
	event := &prototypes.FirehoseEvent{
		UpstreamSeq: 100,
		EventType:   "#commit",
		RawEvent:    []byte("shared-event"),
	}
	err := m.WriteEvent(ctx, event)
	require.NoError(t, err)

	// Connect multiple subscribers with cursor=0 to receive historical events
	conn1 := dialWebSocket(t, server, "0")
	defer conn1.Close() //nolint:errcheck
	conn2 := dialWebSocket(t, server, "0")
	defer conn2.Close() //nolint:errcheck

	// Both should receive the event
	for _, conn := range []*websocket.Conn{conn1, conn2} {
		_, data, err := readMessage(t, conn, 5*time.Second)
		require.NoError(t, err)
		require.Equal(t, []byte("shared-event"), data)
	}
}

func TestSubscribeRepos_LargeCatchup(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	s, m := testServer(t)
	server := startTestServer(t, s)
	defer server.Close()

	// Write many events to simulate a large catchup scenario
	// This verifies that streaming works correctly and doesn't hit FDB transaction limits
	const numEvents = 500
	for i := range numEvents {
		event := &prototypes.FirehoseEvent{
			UpstreamSeq: int64(1000 + i),
			EventType:   "#commit",
			RawEvent:    []byte("event-data-" + string(rune('A'+(i%26)))),
		}
		err := m.WriteEvent(ctx, event)
		require.NoError(t, err)
	}

	// Connect with cursor=0 to receive all historical events
	conn := dialWebSocket(t, server, "0")
	defer conn.Close() //nolint:errcheck

	// Should receive all events in order
	for i := range numEvents {
		msgType, data, err := readMessage(t, conn, 10*time.Second)
		require.NoError(t, err, "failed to receive event %d", i)
		require.Equal(t, websocket.BinaryMessage, msgType)
		expected := []byte("event-data-" + string(rune('A'+(i%26))))
		require.Equal(t, expected, data, "event %d mismatch", i)
	}
}

func TestSubscribeRepos_SubscriberTracking(t *testing.T) {
	t.Parallel()

	s, _ := testServer(t)
	server := startTestServer(t, s)
	defer server.Close()

	// Initially no subscribers
	s.subscribersMu.Lock()
	require.Len(t, s.subscribers, 0)
	s.subscribersMu.Unlock()

	// Connect
	conn := dialWebSocket(t, server, "")

	// Wait a bit for the connection to be registered
	time.Sleep(50 * time.Millisecond)

	// Should have one subscriber
	s.subscribersMu.Lock()
	require.Len(t, s.subscribers, 1)
	s.subscribersMu.Unlock()

	// Disconnect
	_ = conn.Close() //nolint:errcheck

	// Wait for cleanup
	time.Sleep(50 * time.Millisecond)

	// Should have no subscribers
	s.subscribersMu.Lock()
	require.Len(t, s.subscribers, 0)
	s.subscribersMu.Unlock()
}

func TestSubscribeRepos_GracefulShutdown(t *testing.T) {
	t.Parallel()

	s, _ := testServer(t)
	server := startTestServer(t, s)
	defer server.Close()

	// Connect multiple subscribers (no events written, so they'll just wait)
	conn1 := dialWebSocket(t, server, "")
	conn2 := dialWebSocket(t, server, "")

	// Wait for connections to be registered
	time.Sleep(50 * time.Millisecond)

	// Verify we have 2 subscribers
	s.subscribersMu.Lock()
	require.Len(t, s.subscribers, 2)
	s.subscribersMu.Unlock()

	// Send close frames to all subscribers
	s.closeAllSubscribers()

	// Both connections should receive close frames and ReadMessage should error
	_ = conn1.SetReadDeadline(time.Now().Add(5 * time.Second)) //nolint:errcheck
	_, _, err := conn1.ReadMessage()
	require.Error(t, err)

	_ = conn2.SetReadDeadline(time.Now().Add(5 * time.Second)) //nolint:errcheck
	_, _, err = conn2.ReadMessage()
	require.Error(t, err)
}
