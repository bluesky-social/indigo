package firehose

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/gorilla/websocket"
	"github.com/ipfs/go-cid"
	mh "github.com/multiformats/go-multihash"
	"github.com/stretchr/testify/require"
)

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
