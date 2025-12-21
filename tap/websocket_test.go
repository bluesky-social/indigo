package tap

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	"github.com/stretchr/testify/require"
)

var upgrader = websocket.Upgrader{}

func TestWebsocketRecordEvent(t *testing.T) {
	t.Parallel()
	ctx := t.Context()

	recordEvent := Event{
		ID:   1,
		Type: "record",
		record: &RecordEvent{
			DID:        "did:plc:test",
			Collection: "app.bsky.feed.post",
			Rkey:       "abc",
			Action:     "create",
			CID:        "bafytest",
			Record:     json.RawMessage(`{"text":"hello world"}`),
			Live:       true,
		},
	}

	var received *Event
	var wg sync.WaitGroup
	wg.Add(1)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(w, r, nil)
		require.NoError(t, err)
		defer conn.Close()

		buf, _ := json.Marshal(recordEvent)
		conn.WriteMessage(websocket.TextMessage, buf)

		time.Sleep(50 * time.Millisecond)
		conn.WriteMessage(websocket.CloseMessage, websocket.FormatCloseMessage(websocket.CloseNormalClosure, ""))
	}))
	defer server.Close()

	wsURL := "ws://" + strings.TrimPrefix(server.URL, "http://")

	ws, err := NewWebsocket(ctx, wsURL, func(ctx context.Context, ev *Event) {
		received = ev
		wg.Done()
	}, WithLogger(nil))
	require.NoError(t, err)

	go ws.Run(ctx)
	wg.Wait()

	require.NotNil(t, received)
	require.Equal(t, uint64(1), received.ID)
	require.Equal(t, "record", received.Type)

	payload, ok := received.Payload().(*RecordEvent)
	require.True(t, ok)
	require.Equal(t, "did:plc:test", payload.DID)
}

func TestWebsocketUserEvent(t *testing.T) {
	t.Parallel()
	ctx := t.Context()

	userEvent := Event{
		ID:   2,
		Type: "user",
		user: &UserEvent{
			DID:      "did:plc:user456",
			Handle:   "test.bsky.social",
			IsActive: true,
			Status:   "active",
		},
	}

	var received *Event
	var wg sync.WaitGroup
	wg.Add(1)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer conn.Close()

		buf, _ := json.Marshal(userEvent)
		conn.WriteMessage(websocket.TextMessage, buf)

		time.Sleep(50 * time.Millisecond)
		conn.WriteMessage(websocket.CloseMessage, websocket.FormatCloseMessage(websocket.CloseNormalClosure, ""))
	}))
	defer server.Close()

	wsURL := "ws://" + strings.TrimPrefix(server.URL, "http://")

	ws, err := NewWebsocket(ctx, wsURL, func(ctx context.Context, ev *Event) {
		received = ev
		wg.Done()
	}, WithLogger(nil))
	require.NoError(t, err)

	go ws.Run(ctx)
	wg.Wait()

	require.NotNil(t, received)

	payload, ok := received.Payload().(*UserEvent)
	require.True(t, ok)
	require.Equal(t, "test.bsky.social", payload.Handle)
}

func TestWebsocketWithAcks(t *testing.T) {
	t.Parallel()
	ctx := t.Context()

	recordEvent := Event{
		ID:   42,
		Type: "record",
		record: &RecordEvent{
			DID:        "did:plc:ack",
			Collection: "app.bsky.feed.like",
			Rkey:       "ack",
			Action:     "create",
		},
	}

	var receivedAck *ackPayload
	var wg sync.WaitGroup
	wg.Add(1)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer conn.Close()

		buf, _ := json.Marshal(recordEvent)
		conn.WriteMessage(websocket.TextMessage, buf)

		_, ackBuf, err := conn.ReadMessage()
		if err == nil {
			receivedAck = &ackPayload{}
			json.Unmarshal(ackBuf, receivedAck)
		}
		wg.Done()

		conn.WriteMessage(websocket.CloseMessage, websocket.FormatCloseMessage(websocket.CloseNormalClosure, ""))
	}))
	defer server.Close()

	wsURL := "ws://" + strings.TrimPrefix(server.URL, "http://")

	ws, err := NewWebsocket(ctx, wsURL, func(ctx context.Context, ev *Event) {}, WithLogger(nil), WithAcks())
	require.NoError(t, err)

	go ws.Run(ctx)
	wg.Wait()

	require.NotNil(t, receivedAck)
	require.Equal(t, "ack", receivedAck.Type)
	require.Equal(t, uint64(42), receivedAck.ID)
}

func TestWebsocketMultipleEvents(t *testing.T) {
	t.Parallel()
	ctx := t.Context()

	events := []Event{
		{ID: 1, Type: "record", record: &RecordEvent{DID: "did:plc:1", Collection: "app.bsky.feed.post"}},
		{ID: 2, Type: "record", record: &RecordEvent{DID: "did:plc:2", Collection: "app.bsky.feed.like"}},
		{ID: 3, Type: "user", user: &UserEvent{DID: "did:plc:3", Handle: "user3.test"}},
	}

	var received []*Event
	var mu sync.Mutex
	var wg sync.WaitGroup
	wg.Add(len(events))

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer conn.Close()

		for _, ev := range events {
			buf, _ := json.Marshal(ev)
			conn.WriteMessage(websocket.TextMessage, buf)
			time.Sleep(10 * time.Millisecond)
		}

		time.Sleep(50 * time.Millisecond)
		conn.WriteMessage(websocket.CloseMessage, websocket.FormatCloseMessage(websocket.CloseNormalClosure, ""))
	}))
	defer server.Close()

	wsURL := "ws://" + strings.TrimPrefix(server.URL, "http://")

	ws, err := NewWebsocket(ctx, wsURL, func(ctx context.Context, ev *Event) {
		mu.Lock()
		received = append(received, ev)
		mu.Unlock()
		wg.Done()
	}, WithLogger(nil))
	require.NoError(t, err)

	go ws.Run(ctx)
	wg.Wait()

	require.Len(t, received, 3)
	for i, ev := range received {
		require.Equal(t, uint64(i+1), ev.ID)
	}
}
