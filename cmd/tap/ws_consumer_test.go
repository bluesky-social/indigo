package main

import (
	"encoding/json"
	"sync"
	"time"

	"github.com/gorilla/websocket"
)

// testConsumer is a WebSocket client that connects to the /channel endpoint
// and collects received events for test assertions.
type testConsumer struct {
	conn     *websocket.Conn
	messages []MarshallableEvt
	mu       sync.Mutex
	done     chan struct{}
}

func newTestConsumer(url string) (*testConsumer, error) {
	conn, _, err := websocket.DefaultDialer.Dial(url, nil)
	if err != nil {
		return nil, err
	}

	tc := &testConsumer{
		conn: conn,
		done: make(chan struct{}),
	}

	go tc.readLoop()

	return tc, nil
}

func (tc *testConsumer) readLoop() {
	defer close(tc.done)
	for {
		_, message, err := tc.conn.ReadMessage()
		if err != nil {
			return
		}

		var evt MarshallableEvt
		if err := json.Unmarshal(message, &evt); err != nil {
			continue
		}

		tc.mu.Lock()
		tc.messages = append(tc.messages, evt)
		tc.mu.Unlock()
	}
}

// waitForMessages polls until the consumer has received at least count messages
// or the timeout expires. Returns all received messages.
func (tc *testConsumer) waitForMessages(count int, timeout time.Duration) []MarshallableEvt {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		tc.mu.Lock()
		n := len(tc.messages)
		if n >= count {
			result := make([]MarshallableEvt, n)
			copy(result, tc.messages)
			tc.mu.Unlock()
			return result
		}
		tc.mu.Unlock()
		time.Sleep(10 * time.Millisecond)
	}
	tc.mu.Lock()
	defer tc.mu.Unlock()
	result := make([]MarshallableEvt, len(tc.messages))
	copy(result, tc.messages)
	return result
}

// sendAck writes a WsResponse ack back to the server.
func (tc *testConsumer) sendAck(id uint) error {
	return tc.conn.WriteJSON(WsResponse{
		Type: WsResponseAck,
		ID:   id,
	})
}

func (tc *testConsumer) close() {
	tc.conn.Close()
	<-tc.done
}
