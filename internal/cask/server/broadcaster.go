package server

import (
	"bytes"
	"context"
	"log/slog"
	"sync"
	"sync/atomic"
	"time"

	"github.com/bluesky-social/indigo/internal/cask/metrics"
	"github.com/bluesky-social/indigo/internal/cask/models"
	"github.com/gorilla/websocket"
)

const (
	// Size of the per-subscriber event channel buffer.
	broadcastChanSize = 512
)

// broadcaster polls FDB once and fans out events to all at-tip subscribers.
// This replaces per-subscriber polling with a single reader goroutine.
type broadcaster struct {
	log    *slog.Logger
	models *models.Models

	// mu protects subs and liveCursor atomically — addSubscriber reads
	// liveCursor and registers the sub under the same lock used by fanout,
	// guaranteeing a gap-free handoff.
	mu         sync.Mutex
	subs       map[*broadcastSub]struct{}
	liveCursor []byte
}

// broadcastSub is a subscriber registered with the broadcaster.
type broadcastSub struct {
	ch      chan *broadcastEvent // buffered event channel
	kickCh  chan struct{}        // closed when subscriber is too slow
	done    <-chan struct{}      // subscriber's ctx.Done()
	tooSlow atomic.Bool          // set once; skips future fanouts
}

// broadcastEvent carries a single event from the broadcaster to a subscriber.
type broadcastEvent struct {
	rawEvent []byte // raw CBOR bytes to write to the WebSocket
	cursor   []byte // event cursor for dedup at the bridge→channel boundary
}

func newBroadcaster(log *slog.Logger, m *models.Models) *broadcaster {
	return &broadcaster{
		log:    log,
		models: m,
		subs:   make(map[*broadcastSub]struct{}),
	}
}

// Run is the broadcaster's main loop. It initializes the live cursor, then
// polls FDB in a tight loop and fans out events to all registered subscribers.
func (b *broadcaster) Run(ctx context.Context) {
	// Initialize liveCursor to the current tip so we only broadcast new events.
	cursor, err := b.models.GetLatestVersionstamp(ctx)
	if err != nil {
		b.log.Error("broadcaster: failed to get initial cursor", "error", err)
		return
	}

	b.mu.Lock()
	b.liveCursor = cursor
	b.mu.Unlock()

	b.log.Info("broadcaster started")

	for {
		select {
		case <-ctx.Done():
			b.log.Info("broadcaster stopped")
			return
		default:
		}

		results, nextCursor, err := b.models.GetEventsSince(ctx, cursor, eventBatchSize)
		if err != nil {
			if ctx.Err() != nil {
				return
			}
			b.log.Error("broadcaster: FDB read failed", "error", err)
			time.Sleep(pollInterval)
			continue
		}

		if len(results) == 0 {
			select {
			case <-ctx.Done():
				return
			case <-time.After(pollInterval):
			}
			continue
		}

		b.mu.Lock()
		for _, r := range results {
			b.fanout(&broadcastEvent{
				rawEvent: r.Event.RawEvent,
				cursor:   r.Cursor,
			})
		}
		b.liveCursor = nextCursor
		cursor = nextCursor
		b.mu.Unlock()
	}
}

// fanout sends an event to all registered subscribers. Must be called under mu.
// Slow subscribers (full channel) are marked and kicked.
func (b *broadcaster) fanout(evt *broadcastEvent) {
	for sub := range b.subs {
		if sub.tooSlow.Load() {
			continue
		}

		select {
		case sub.ch <- evt:
		default:
			// Buffer full — mark as too slow and kick
			sub.tooSlow.Store(true)
			close(sub.kickCh)
			metrics.SlowSubscriberDisconnects.Inc()
			b.log.Warn("broadcaster: kicking slow subscriber")
		}
	}
}

// addSubscriber atomically registers a new subscriber and returns the current
// liveCursor. The caller must bridge-read from its own cursor to the returned
// cursor, then switch to reading from the channel. Because registration and
// liveCursor read happen under the same lock used by fanout, no events are
// lost or duplicated.
func (b *broadcaster) addSubscriber(done <-chan struct{}) (*broadcastSub, []byte) {
	b.mu.Lock()
	defer b.mu.Unlock()

	sub := &broadcastSub{
		ch:     make(chan *broadcastEvent, broadcastChanSize),
		kickCh: make(chan struct{}),
		done:   done,
	}
	b.subs[sub] = struct{}{}
	metrics.BroadcastSubscribers.Inc()

	// Return a copy of liveCursor so the caller can bridge-read safely.
	cursorCopy := make([]byte, len(b.liveCursor))
	copy(cursorCopy, b.liveCursor)
	return sub, cursorCopy
}

// removeSubscriber unregisters a subscriber from the broadcaster.
func (b *broadcaster) removeSubscriber(sub *broadcastSub) {
	b.mu.Lock()
	delete(b.subs, sub)
	b.mu.Unlock()
	metrics.BroadcastSubscribers.Dec()
}

// streamFromBroadcaster transitions a subscriber from FDB-based catchup to
// channel-based broadcast streaming. It performs a bridge read to cover
// any gap, then reads from the broadcast channel.
func (s *Server) streamFromBroadcaster(ctx context.Context, sub *subscriber, conn *websocket.Conn, lastCursor []byte) error {
	bsub, registrationCursor := s.broadcaster.addSubscriber(ctx.Done())
	defer s.broadcaster.removeSubscriber(bsub)

	// Bridge read: catch up from lastCursor to registrationCursor.
	// If lastCursor >= registrationCursor, the subscriber is already ahead
	// of the broadcaster (race window) — skip the bridge read.
	if registrationCursor != nil && (lastCursor == nil || bytes.Compare(lastCursor, registrationCursor) < 0) {
		bridgeCursor := lastCursor
		for {
			select {
			case <-ctx.Done():
				return nil
			default:
			}

			nextCursor, err := s.processBatch(ctx, sub, conn, bridgeCursor)
			if err != nil {
				return err
			}
			if nextCursor == nil {
				break // caught up
			}
			bridgeCursor = nextCursor
			lastCursor = nextCursor

			// Stop once we've reached or passed the registration cursor
			if bytes.Compare(nextCursor, registrationCursor) >= 0 {
				break
			}
		}
	}

	// Channel read loop — events from the broadcaster.
	// Skip any events we already sent during catchup or bridge read.
	for {
		select {
		case evt := <-bsub.ch:
			// Dedup: skip events at or before the last cursor we wrote
			if lastCursor != nil && bytes.Compare(evt.cursor, lastCursor) <= 0 {
				continue
			}

			_ = conn.SetWriteDeadline(time.Now().Add(30 * time.Second))
			if err := conn.WriteMessage(websocket.BinaryMessage, evt.rawEvent); err != nil {
				return err
			}
			sub.eventsSent.Add(1)
			metrics.EventsSentTotal.Inc()
			lastCursor = evt.cursor

		case <-bsub.kickCh:
			s.log.Warn("subscriber kicked (too slow)",
				"subscriber_id", sub.id,
				"remote_addr", sub.remoteAddr,
			)
			return nil

		case <-ctx.Done():
			return nil
		}
	}
}
