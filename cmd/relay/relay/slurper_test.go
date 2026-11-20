package relay

import (
	"bytes"
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"testing/synctest"
	"time"

	comatproto "github.com/bluesky-social/indigo/api/atproto"
	"github.com/bluesky-social/indigo/cmd/relay/relay/models"
	"github.com/bluesky-social/indigo/cmd/relay/stream"

	"github.com/RussellLuo/slidingwindow"
	"github.com/gorilla/websocket"
	"github.com/stretchr/testify/require"
	cbg "github.com/whyrusleeping/cbor-gen"
)

func TestSubscriptionRetry(t *testing.T) {
	var retry subscriptionRetry
	require.Zero(t, retry.delay(), "the initial dial should be immediate")
	for stage := 1; stage <= 12; stage++ {
		retry.failed(time.Second)
		if stage < 10 {
			for range 100 {
				delay := retry.delay()
				require.GreaterOrEqual(t, delay, time.Duration(stage)*2*time.Second)
				require.Less(t, delay, time.Duration(stage)*2*time.Second+time.Second)
			}
		} else {
			require.Equal(t, 10, retry.failures, "repeated failures must not overflow the counter")
			require.Equal(t, 30*time.Second, retry.delay())
		}
	}

	retry.failed(stableSubscriptionDuration - time.Nanosecond)
	require.Equal(t, 10, retry.failures, "short connections do not reset the delay")
	retry.failed(stableSubscriptionDuration)
	require.Equal(t, 1, retry.failures, "a stable connection starts a fresh retry sequence")
	require.GreaterOrEqual(t, retry.delay(), 2*time.Second)
	require.Less(t, retry.delay(), 3*time.Second)
}

func TestWaitForRetry(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		require.NoError(t, waitForRetry(ctx, 0))
		started := time.Now()
		require.NoError(t, waitForRetry(ctx, 30*time.Second))
		require.Equal(t, 30*time.Second, time.Since(started))

		done := make(chan error, 1)
		go func() { done <- waitForRetry(ctx, 30*time.Second) }()
		synctest.Wait()
		started = time.Now()
		cancel()
		require.ErrorIs(t, <-done, context.Canceled)
		require.Zero(t, time.Since(started), "cancellation must interrupt an active wait")
		require.ErrorIs(t, waitForRetry(ctx, 0), context.Canceled)
	})
}

// These are three distinct ways for a successful handshake to be followed by
// a rejected event. Each must back off while retaining the last processed cursor.
func TestSlurperBacksOffRejectedStreams(t *testing.T) {
	var commit bytes.Buffer
	require.NoError(t, (&stream.EventHeader{Op: stream.EvtKindMessage, MsgType: "#commit"}).MarshalCBOR(&commit))
	cw := cbg.NewCborWriter(&commit)
	require.NoError(t, cw.WriteMajorTypeHeader(cbg.MajMap, 1))
	require.NoError(t, cw.WriteMajorTypeHeader(cbg.MajTextString, uint64(len("blocks"))))
	_, err := cw.Write([]byte("blocks"))
	require.NoError(t, err)
	const blocksSize = 2*1024*1024 + 1
	require.NoError(t, cw.WriteMajorTypeHeader(cbg.MajByteString, blocksSize))
	_, err = cw.Write(make([]byte, blocksSize))
	require.NoError(t, err)
	require.Less(t, commit.Len(), stream.MaxMessageBytes)

	for _, tc := range []struct {
		name    string
		payload []byte
	}{
		{"malformed_cbor", []byte{0xff}},
		{"websocket_read_limit", make([]byte, stream.MaxMessageBytes+1)},
		{"commit_decoder_limit", commit.Bytes()},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			type attempt struct {
				at     time.Time
				cursor string
			}
			attempts := make(chan attempt, 8)
			processed := make(chan int64, 1)
			releaseProcessing := make(chan struct{})
			var numAttempts atomic.Int32
			upgrader := websocket.Upgrader{}
			valid := serializeSlurperTestEvent(t, &stream.XRPCStreamEvent{
				RepoIdentity: &comatproto.SyncSubscribeRepos_Identity{
					Did: "did:plc:example", Seq: 42, Time: "2026-10-20T00:00:00Z",
				},
			})
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				n := numAttempts.Add(1)
				attempts <- attempt{time.Now(), r.URL.Query().Get("cursor")}
				conn, err := upgrader.Upgrade(w, r, nil)
				if err != nil {
					return
				}
				defer conn.Close()
				_ = conn.SetWriteDeadline(time.Now().Add(2 * time.Second))
				if n == 2 {
					_ = conn.WriteMessage(websocket.BinaryMessage, valid)
				}
				if n <= 2 {
					// WriteMessage sends one frame, so the WebSocket limit is
					// checked before the payload can fail CBOR decoding.
					_ = conn.WriteMessage(websocket.BinaryMessage, tc.payload)
				}
				for {
					if _, _, err := conn.ReadMessage(); err != nil {
						return
					}
				}
			}))
			defer server.Close()

			persisted := make(chan HostCursor, 8)
			statuses := make(chan models.HostStatus, 8)
			config := DefaultSlurperConfig()
			config.PersistCursorCallback = func(_ context.Context, cursors *[]HostCursor) error {
				for _, cursor := range *cursors {
					persisted <- cursor
				}
				return nil
			}
			config.PersistHostStatusCallback = func(_ context.Context, _ uint64, state models.HostStatus) error {
				statuses <- state
				return nil
			}
			s, sub, done := startSlurperTestSubscription(t, server.URL, 41, config, func(_ context.Context, evt *stream.XRPCStreamEvent, _ string, _ uint64) error {
				processed <- evt.Sequence()
				<-releaseProcessing
				return nil
			})
			defer sub.cancel()
			// Always release the worker before cleanup, including on test failure.
			defer func() {
				select {
				case <-releaseProcessing:
				default:
					close(releaseProcessing)
				}
			}()

			first := receiveSlurperTestValue(t, attempts)
			require.Equal(t, "41", first.cursor)
			second := receiveSlurperTestValue(t, attempts)
			require.Equal(t, "41", second.cursor, "a rejected event must not advance the cursor")
			require.GreaterOrEqual(t, second.at.Sub(first.at), 2*time.Second)
			require.Equal(t, int64(42), receiveSlurperTestValue(t, processed))
			close(releaseProcessing)

			third := receiveSlurperTestValue(t, attempts)
			require.Equal(t, "42", third.cursor, "reconnect must use the cursor after all queued work drains")
			require.GreaterOrEqual(t, third.at.Sub(second.at), 4*time.Second, "short connections with progress must not reset backoff")
			require.Equal(t, HostCursor{HostID: 1, LastSeq: 42}, receiveSlurperTestValue(t, persisted))
			require.True(t, s.CheckIfSubscribed(sub.Hostname), "stream failures must keep the host subscribed")
			select {
			case state := <-statuses:
				t.Fatalf("stream failure unexpectedly changed host status to %q", state)
			default:
			}
			sub.cancel()
			receiveSlurperTestValue(t, done)
		})
	}
}

func TestSlurperTerminalResponses(t *testing.T) {
	for _, tc := range []struct {
		name       string
		server     string
		event      *stream.XRPCStreamEvent
		wantStatus models.HostStatus
	}{
		{
			name: "future_cursor", event: &stream.XRPCStreamEvent{Error: &stream.ErrorFrame{Error: "FutureCursor"}},
			wantStatus: models.HostStatusIdle,
		},
		{name: "relay_header", server: "indigo-atproto-relay", wantStatus: models.HostStatusBanned},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var payload []byte
			if tc.event != nil {
				payload = serializeSlurperTestEvent(t, tc.event)
			}
			closed := make(chan struct{}, 4)
			var attempts atomic.Int32
			upgrader := websocket.Upgrader{}
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				attempts.Add(1)
				conn, err := upgrader.Upgrade(w, r, http.Header{"Server": []string{tc.server}})
				if err != nil {
					return
				}
				defer conn.Close()
				if payload != nil {
					_ = conn.WriteMessage(websocket.BinaryMessage, payload)
				}
				_, _, _ = conn.ReadMessage()
				closed <- struct{}{}
			}))
			defer server.Close()
			status := make(chan models.HostStatus, 1)
			config := DefaultSlurperConfig()
			config.PersistHostStatusCallback = func(_ context.Context, _ uint64, state models.HostStatus) error {
				status <- state
				return nil
			}
			_, sub, done := startSlurperTestSubscription(t, server.URL, 41, config, func(context.Context, *stream.XRPCStreamEvent, string, uint64) error { return nil })
			defer sub.cancel()
			require.Equal(t, tc.wantStatus, receiveSlurperTestValue(t, status))
			receiveSlurperTestValue(t, done)
			receiveSlurperTestValue(t, closed)
			require.Equal(t, int32(1), attempts.Load())
		})
	}
}

func serializeSlurperTestEvent(t *testing.T, evt *stream.XRPCStreamEvent) []byte {
	t.Helper()
	var buf bytes.Buffer
	require.NoError(t, evt.Serialize(&buf))
	return buf.Bytes()
}

// Start the production redial loop directly so its completion can be awaited;
// no periodic persistence goroutine is needed for these connection tests.
func startSlurperTestSubscription(t *testing.T, serverURL string, cursor int64, config *SlurperConfig, process ProcessMessageFunc) (*Slurper, *Subscription, <-chan struct{}) {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	limiter := func(window time.Duration) *slidingwindow.Limiter {
		lim, _ := slidingwindow.NewLimiter(window, 1000, windowFunc)
		return lim
	}
	host := &models.Host{ID: 1, Hostname: strings.TrimPrefix(serverURL, "http://"), NoSSL: true, LastSeq: cursor}
	sub := &Subscription{
		Hostname: host.Hostname, HostID: host.ID, ctx: ctx, cancel: cancel,
		Limiters: &StreamLimiters{PerSecond: limiter(time.Second), PerHour: limiter(time.Hour), PerDay: limiter(24 * time.Hour)},
	}
	sub.LastSeq.Store(cursor)
	s := &Slurper{
		Config: config, processCallback: process, subs: map[string]*Subscription{host.Hostname: sub},
		logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
	}
	done := make(chan struct{})
	go func() {
		s.subscribeWithRedialer(ctx, host, sub)
		close(done)
	}()
	t.Cleanup(func() {
		cancel()
		receiveSlurperTestValue(t, done)
	})
	return s, sub, done
}

func receiveSlurperTestValue[T any](t *testing.T, ch <-chan T) T {
	t.Helper()
	select {
	case value := <-ch:
		return value
	case <-time.After(12 * time.Second):
		t.Fatal("timed out waiting for subscription")
		var zero T
		return zero
	}
}
