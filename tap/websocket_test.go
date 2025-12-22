package tap

import (
	"context"
	"encoding/json"
	"errors"
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

func TestWebsocket(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	require := require.New(t)

	events := []Event{
		{ID: 1, Type: eventTypeRecord, record: &RecordEvent{DID: "did:plc:1", Collection: "app.bsky.feed.post"}},
		{ID: 2, Type: eventTypeRecord, record: &RecordEvent{DID: "did:plc:2", Collection: "app.bsky.feed.like"}},
		{ID: 3, Type: eventTypeUser, user: &UserEvent{DID: "did:plc:3", Handle: "user3.test"}},
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

	ws, err := NewWebsocket(wsURL, func(ctx context.Context, ev *Event) error {
		mu.Lock()
		received = append(received, ev)
		mu.Unlock()
		wg.Done()
		return nil
	}, WithLogger(nil))
	require.NoError(err)

	go ws.Run(ctx)
	wg.Wait()

	require.Len(received, 3)
	for i, ev := range received {
		require.Equal(uint64(i+1), ev.ID)

		switch i {
		case 0, 1:
			switch pl := ev.Payload().(type) {
			case *RecordEvent:
				require.NotNil(events[i].record)
				require.Equal(events[i].record.Collection, pl.Collection)
				require.Equal(events[i].Type, eventTypeRecord)
			default:
				require.FailNow("incorrect payload type, want %T got %T", &RecordEvent{}, ev.Payload())
			}

		case 2:
			switch pl := ev.Payload().(type) {
			case *UserEvent:
				require.NotNil(events[i].user)
				require.Equal(events[i].user.Handle, pl.Handle)
				require.Equal(events[i].Type, eventTypeUser)
			default:
				require.FailNow("incorrect payload type, want %T got %T", &UserEvent{}, ev.Payload())
			}
		}
	}
}

func TestWebsocketWithAcks(t *testing.T) {
	t.Parallel()

	t.Run("ack sent on success", func(t *testing.T) {
		t.Parallel()
		ctx := t.Context()
		require := require.New(t)

		recordEvent := Event{
			ID:   42,
			Type: eventTypeRecord,
			record: &RecordEvent{
				DID:        "did:plc:ack",
				Collection: "app.bsky.feed.like",
				Rkey:       "ack",
				Action:     "create",
			},
		}

		var receivedAck *Event
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
				receivedAck = &Event{}
				json.Unmarshal(ackBuf, receivedAck)
			}
			wg.Done()

			conn.WriteMessage(websocket.CloseMessage, websocket.FormatCloseMessage(websocket.CloseNormalClosure, ""))
		}))
		defer server.Close()

		wsURL := "ws://" + strings.TrimPrefix(server.URL, "http://")

		ws, err := NewWebsocket(wsURL, func(ctx context.Context, ev *Event) error {
			return nil
		}, WithLogger(nil), WithAcks())
		require.NoError(err)

		go ws.Run(ctx)
		wg.Wait()

		require.NotNil(receivedAck)
		require.Equal(eventTypeACK, receivedAck.Type)
		require.Equal(recordEvent.ID, receivedAck.ID)
	})

	t.Run("ack not sent on error", func(t *testing.T) {
		t.Parallel()
		ctx := t.Context()
		require := require.New(t)

		recordEvent := Event{
			ID:   99,
			Type: eventTypeRecord,
			record: &RecordEvent{
				DID:        "did:plc:noack",
				Collection: "app.bsky.feed.post",
				Rkey:       "noack",
				Action:     "create",
			},
		}

		var receivedAck bool
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

			conn.SetReadDeadline(time.Now().Add(100 * time.Millisecond))
			_, _, err = conn.ReadMessage()
			receivedAck = err == nil
			wg.Done()

			conn.WriteMessage(websocket.CloseMessage, websocket.FormatCloseMessage(websocket.CloseNormalClosure, ""))
		}))
		defer server.Close()

		wsURL := "ws://" + strings.TrimPrefix(server.URL, "http://")

		ws, err := NewWebsocket(wsURL, func(ctx context.Context, ev *Event) error {
			return errors.New("processing failed")
		}, WithLogger(nil), WithAcks())
		require.NoError(err)

		go ws.Run(ctx)
		wg.Wait()

		require.False(receivedAck, "expected no ACK when handler returns error")
	})
}
