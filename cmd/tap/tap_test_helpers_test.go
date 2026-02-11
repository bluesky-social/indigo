package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/bluesky-social/indigo/atproto/identity"
	"github.com/bluesky-social/indigo/cmd/tap/models"
	"github.com/gorilla/websocket"
	"gorm.io/gorm"
)

type testEnvOpts struct {
	outboxMode     OutboxMode
	webhookURL     string
	retryTimeout   time.Duration
	eventCacheSize int
}

type testEnv struct {
	t      *testing.T
	ctx    context.Context
	cancel context.CancelFunc
	db     *gorm.DB
	events *EventManager
	outbox *Outbox
	server *TapServer
	repos  *RepoManager
	idDir  *identity.MockDirectory
	port   int
}

func newTestEnv(t *testing.T, opts testEnvOpts) *testEnv {
	t.Helper()

	ctx, cancel := context.WithCancel(t.Context())

	db, err := SetupDatabase("sqlite://file::memory:?cache=shared", 1)
	if err != nil {
		cancel()
		t.Fatalf("failed to setup test database: %v", err)
	}

	cacheSize := opts.eventCacheSize
	if cacheSize == 0 {
		cacheSize = 1000
	}

	retryTimeout := opts.retryTimeout
	if retryTimeout == 0 {
		retryTimeout = 60 * time.Second
	}

	webhookURL := opts.webhookURL
	disableAcks := false
	switch opts.outboxMode {
	case OutboxModeFireAndForget:
		disableAcks = true
	case OutboxModeWebhook:
		// webhookURL must be set by caller
	case OutboxModeWebsocketAck:
		// default
	default:
		// default to fire-and-forget for simplicity
		disableAcks = true
	}

	config := &TapConfig{
		EventCacheSize:    cacheSize,
		OutboxParallelism: 1,
		DisableAcks:       disableAcks,
		WebhookURL:        webhookURL,
		RetryTimeout:      retryTimeout,
	}

	logger := slog.Default().With("test", t.Name())

	idDir := identity.NewMockDirectory()
	events := NewEventManager(logger, db, config)
	repos := NewRepoManager(logger, db, events, idDir)
	outbox := NewOutbox(logger, events, config)
	server := NewTapServer(logger, db, outbox, idDir, nil, nil, config)

	// Find a free port
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		cancel()
		t.Fatalf("failed to find free port: %v", err)
	}
	port := listener.Addr().(*net.TCPAddr).Port
	listener.Close()

	// Start background goroutines
	go events.LoadEvents(ctx)
	go outbox.Run(ctx)

	// Start HTTP server
	go func() {
		if err := server.Start(fmt.Sprintf("127.0.0.1:%d", port)); err != nil && err != http.ErrServerClosed {
			select {
			case <-ctx.Done():
			default:
				t.Logf("server error: %v", err)
			}
		}
	}()

	// Wait for the server to be ready
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		conn, err := net.DialTimeout("tcp", fmt.Sprintf("127.0.0.1:%d", port), 50*time.Millisecond)
		if err == nil {
			conn.Close()
			break
		}
		time.Sleep(5 * time.Millisecond)
	}

	// Wait for event manager to finish loading
	events.WaitForReady(ctx)

	te := &testEnv{
		t:      t,
		ctx:    ctx,
		cancel: cancel,
		db:     db,
		events: events,
		outbox: outbox,
		server: server,
		repos:  repos,
		idDir:  idDir,
		port:   port,
	}

	t.Cleanup(func() {
		cancel()
		server.Shutdown(context.Background())
		sqlDB, err := db.DB()
		if err == nil {
			sqlDB.Close()
		}
	})

	return te
}

func (te *testEnv) baseURL() string {
	return fmt.Sprintf("http://127.0.0.1:%d", te.port)
}

func (te *testEnv) wsURL() string {
	return fmt.Sprintf("ws://127.0.0.1:%d/channel", te.port)
}

// pushRecordEvents creates and pushes record events through the EventManager.
// Returns the event IDs that were generated.
func (te *testEnv) pushRecordEvents(did string, count int, live bool) []uint {
	te.t.Helper()

	evts := make([]*RecordEvt, count)
	for i := 0; i < count; i++ {
		evts[i] = &RecordEvt{
			Live:       live,
			Did:        did,
			Rev:        fmt.Sprintf("rev-%d", i),
			Collection: "app.bsky.feed.post",
			Rkey:       fmt.Sprintf("rkey-%d-%d", time.Now().UnixNano(), i),
			Action:     "create",
			Record:     map[string]interface{}{"text": fmt.Sprintf("test post %d", i)},
			Cid:        fmt.Sprintf("cid-%d-%d", time.Now().UnixNano(), i),
		}
	}

	startID := uint(te.events.nextID.Load())

	err := te.events.AddRecordEvents(te.ctx, evts, live, func(tx *gorm.DB) error {
		return nil
	})
	if err != nil {
		te.t.Fatalf("failed to push record events: %v", err)
	}

	ids := make([]uint, count)
	for i := 0; i < count; i++ {
		ids[i] = startID + uint(i) + 1
	}
	return ids
}

// pushIdentityEvent pushes a single identity event.
func (te *testEnv) pushIdentityEvent(did, handle string, status models.AccountStatus) uint {
	te.t.Helper()

	startID := uint(te.events.nextID.Load())

	evt := &IdentityEvt{
		Did:      did,
		Handle:   handle,
		IsActive: status == models.AccountStatusActive,
		Status:   status,
	}

	err := te.events.AddIdentityEvent(te.ctx, evt, func(tx *gorm.DB) error {
		return nil
	})
	if err != nil {
		te.t.Fatalf("failed to push identity event: %v", err)
	}

	return startID + 1
}

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
		time.Sleep(5 * time.Millisecond)
	}
	tc.mu.Lock()
	defer tc.mu.Unlock()
	result := make([]MarshallableEvt, len(tc.messages))
	copy(result, tc.messages)
	return result
}

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

// testWebhookReceiver is an HTTP server that collects webhook POST bodies.
type testWebhookReceiver struct {
	server   *httptest.Server
	received [][]byte
	mu       sync.Mutex
}

func newTestWebhookReceiver() *testWebhookReceiver {
	r := &testWebhookReceiver{}
	r.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		if req.Method != http.MethodPost {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		buf, _ := io.ReadAll(req.Body)
		req.Body.Close()

		r.mu.Lock()
		r.received = append(r.received, buf)
		r.mu.Unlock()

		w.WriteHeader(http.StatusOK)
	}))
	return r
}

func (r *testWebhookReceiver) waitForMessages(count int, timeout time.Duration) [][]byte {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		r.mu.Lock()
		n := len(r.received)
		if n >= count {
			result := make([][]byte, n)
			copy(result, r.received)
			r.mu.Unlock()
			return result
		}
		r.mu.Unlock()
		time.Sleep(5 * time.Millisecond)
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	result := make([][]byte, len(r.received))
	copy(result, r.received)
	return result
}

func (r *testWebhookReceiver) close() {
	r.server.Close()
}

// insertRepo inserts a repo directly into the database with the given parameters.
func (te *testEnv) insertRepo(did string, state models.RepoState, rev string, prevData string, handle string) {
	te.t.Helper()
	repo := models.Repo{
		Did:      did,
		State:    state,
		Status:   models.AccountStatusActive,
		Rev:      rev,
		PrevData: prevData,
		Handle:   handle,
	}
	if err := te.db.Create(&repo).Error; err != nil {
		te.t.Fatalf("failed to insert repo: %v", err)
	}
}
