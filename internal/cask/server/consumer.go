package server

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"time"

	"github.com/bluesky-social/indigo/api/atproto"
	"github.com/bluesky-social/indigo/events"
	"github.com/bluesky-social/indigo/internal/cask/metrics"
	"github.com/bluesky-social/indigo/internal/cask/models"
	"github.com/bluesky-social/indigo/pkg/types"
	"github.com/gorilla/websocket"
	"google.golang.org/protobuf/types/known/timestamppb"
)

const (
	maxBatchSize = 256
	channelSize  = 256
	flushWait    = 10 * time.Millisecond
)

// parsedEvent holds the result of reading and parsing one websocket message.
// The proto is constructed later in the writer (adds ReceivedAt timestamp at write time).
type parsedEvent struct {
	rawEvent  []byte
	eventType string
	seq       int64
}

type Consumer struct {
	log    *slog.Logger
	models *models.Models
	host   string
}

func newFirehoseConsumer(log *slog.Logger, models *models.Models, host string) *Consumer {
	return &Consumer{
		log:    log.With("component", "firehose-consumer"),
		models: models,
		host:   host,
	}
}

// Run connects to the upstream firehose and consumes events until the context is cancelled.
// Events are stored in FoundationDB as they are received, and fanned out amongst all cask
// processes to ensure we present a consistent view of the world regardless of which backend
// the client connects to.
func (c *Consumer) Run(ctx context.Context) error {
	// Read the last upstream sequence we stored so we can resume from where we left off
	cursor, err := c.models.GetLatestUpstreamSeq(ctx)
	if err != nil {
		return fmt.Errorf("failed to get latest upstream seq: %w", err)
	}

	u, err := url.Parse(c.host)
	if err != nil {
		return fmt.Errorf("invalid firehose URL: %w", err)
	}
	u.Path = "xrpc/com.atproto.sync.subscribeRepos"
	if cursor > 0 {
		u.RawQuery = fmt.Sprintf("cursor=%d", cursor)
	}

	c.log.Info("connecting to upstream firehose", "url", u.String(), "cursor", cursor)

	dialer := websocket.DefaultDialer
	con, _, err := dialer.DialContext(ctx, u.String(), http.Header{
		"User-Agent": []string{"cask/0.1"},
	})
	if err != nil {
		return fmt.Errorf("failed to connect to firehose: %w", err)
	}
	defer con.Close() // nolint:errcheck

	c.log.Info("connected to upstream firehose")

	return c.handleConnection(ctx, con)
}

// handleConnection reads events from the upstream firehose websocket connection and stores them
// in the database to be fanned out amongst all other cask processes. It uses two goroutines:
// a reader that owns websocket reads, and a writer (main goroutine) that batches and writes to FDB.
func (c *Consumer) handleConnection(ctx context.Context, con *websocket.Conn) error {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	metrics.ConsumerConnected.Inc()
	defer metrics.ConsumerConnected.Dec()

	go c.pingHandler(ctx, con)

	// Configure connection
	con.SetPingHandler(func(message string) error {
		err := con.WriteControl(websocket.PongMessage, []byte(message), time.Now().Add(time.Second*60))
		if err == websocket.ErrCloseSent {
			return nil
		}
		return err
	})

	con.SetPongHandler(func(_ string) error {
		if err := con.SetReadDeadline(time.Now().Add(time.Minute)); err != nil {
			c.log.Error("failed to set read deadline", "error", err)
		}
		return nil
	})

	// Channel for parsed events from reader to writer
	eventCh := make(chan *parsedEvent, channelSize)
	errCh := make(chan error, 1)

	// Reader goroutine — owns all websocket reads
	go func() {
		defer close(eventCh)
		for {
			select {
			case <-ctx.Done():
				return
			default:
			}

			pe, err := c.parseEvent(con)
			if err != nil {
				errCh <- err
				return
			}
			eventCh <- pe
		}
	}()

	// Writer (main goroutine) — drains channel, batches, writes to FDB
	writerErr := c.batchWriter(ctx, eventCh)

	// Collect reader error
	var readerErr error
	select {
	case readerErr = <-errCh:
	default:
	}

	// Return first non-nil error
	if readerErr != nil {
		return readerErr
	}
	return writerErr
}

// parseEvent reads a single event from the websocket and parses it into a parsedEvent.
// The header is parsed once; extractSeqFromBody reuses the already-positioned reader.
func (c *Consumer) parseEvent(con *websocket.Conn) (*parsedEvent, error) {
	mt, rawReader, err := con.NextReader()
	if err != nil {
		return nil, fmt.Errorf("websocket read error: %w", err)
	}

	if mt != websocket.BinaryMessage {
		return nil, fmt.Errorf("expected binary message, got %d", mt)
	}

	// Read the entire message into a buffer so we can store the raw bytes
	var buf bytes.Buffer
	if _, err := io.Copy(&buf, rawReader); err != nil {
		return nil, fmt.Errorf("failed to read message: %w", err)
	}
	rawEvent := buf.Bytes()

	metrics.EventSizeBytes.Observe(float64(len(rawEvent)))

	// Parse just the header to extract event type and sequence
	reader := bytes.NewReader(rawEvent)
	var header events.EventHeader
	if err := header.UnmarshalCBOR(reader); err != nil {
		return nil, fmt.Errorf("failed to parse event header: %w", err)
	}

	switch header.Op {
	case events.EvtKindMessage:
		seq := extractSeqFromBody(reader, header.MsgType)
		return &parsedEvent{
			rawEvent:  rawEvent,
			eventType: header.MsgType,
			seq:       seq,
		}, nil
	case events.EvtKindErrorFrame:
		return nil, fmt.Errorf("received firehose error event")
	default:
		return &parsedEvent{
			rawEvent:  rawEvent,
			eventType: header.MsgType,
		}, nil
	}
}

// extractSeqFromBody extracts the sequence number from an already-positioned reader
// (past the header). This avoids re-parsing the header.
func extractSeqFromBody(reader *bytes.Reader, msgType string) int64 {
	switch msgType {
	case "#commit":
		var evt atproto.SyncSubscribeRepos_Commit
		if err := evt.UnmarshalCBOR(reader); err != nil {
			return 0
		}
		return evt.Seq
	case "#identity":
		var evt atproto.SyncSubscribeRepos_Identity
		if err := evt.UnmarshalCBOR(reader); err != nil {
			return 0
		}
		return evt.Seq
	case "#account":
		var evt atproto.SyncSubscribeRepos_Account
		if err := evt.UnmarshalCBOR(reader); err != nil {
			return 0
		}
		return evt.Seq
	case "#sync":
		var evt atproto.SyncSubscribeRepos_Sync
		if err := evt.UnmarshalCBOR(reader); err != nil {
			return 0
		}
		return evt.Seq
	case "#labels":
		var evt atproto.LabelSubscribeLabels_Labels
		if err := evt.UnmarshalCBOR(reader); err != nil {
			return 0
		}
		return evt.Seq
	default:
		return 0
	}
}

// batchWriter drains the event channel, accumulates batches, and writes to FDB.
func (c *Consumer) batchWriter(ctx context.Context, eventCh <-chan *parsedEvent) error {
	for {
		// 1. Block until first event arrives (or channel closes)
		pe, ok := <-eventCh
		if !ok {
			return nil // channel closed, reader is done
		}

		batch := []*parsedEvent{pe}

		// 2. Non-blocking drain of whatever accumulated during the last FDB write
		drainLoop := true
		for drainLoop && len(batch) < maxBatchSize {
			select {
			case pe, ok := <-eventCh:
				if !ok {
					drainLoop = false
				} else {
					batch = append(batch, pe)
				}
			default:
				drainLoop = false
			}
		}

		// 3. If batch is still small, wait briefly for more events
		if len(batch) < maxBatchSize {
			timer := time.NewTimer(flushWait)
			waitLoop := true
			for waitLoop && len(batch) < maxBatchSize {
				select {
				case pe, ok := <-eventCh:
					if !ok {
						waitLoop = false
					} else {
						batch = append(batch, pe)
					}
				case <-timer.C:
					waitLoop = false
				}
			}
			timer.Stop()
		}

		// 4. Flush batch to FDB
		if err := c.writeBatchWithRetry(ctx, batch); err != nil {
			return err
		}
	}
}

// writeBatchWithRetry writes a batch of parsed events to FDB with retry logic.
func (c *Consumer) writeBatchWithRetry(ctx context.Context, batch []*parsedEvent) error {
	// Build proto events
	protoEvents := make([]*types.FirehoseEvent, len(batch))
	for i, pe := range batch {
		protoEvents[i] = &types.FirehoseEvent{
			UpstreamSeq: pe.seq,
			RawEvent:    pe.rawEvent,
			ReceivedAt:  timestamppb.Now(),
			EventType:   pe.eventType,
		}
	}

	const retries = 5
	for retry := range retries {
		start := time.Now()
		err := c.models.WriteEventBatch(ctx, protoEvents)
		dur := time.Since(start)

		if err == nil {
			metrics.ConsumerBatchWriteDuration.Observe(dur.Seconds())
			metrics.ConsumerBatchSize.Observe(float64(len(batch)))

			// Update per-event metrics
			for _, pe := range batch {
				metrics.EventsReceivedTotal.WithLabelValues(pe.eventType, metrics.StatusOK).Inc()
				if pe.seq > 0 {
					metrics.UpstreamSeq.Set(float64(pe.seq))
				}
			}
			return nil
		}

		if retry == retries-1 {
			// Record error metrics for all events in the batch
			for _, pe := range batch {
				metrics.EventsReceivedTotal.WithLabelValues(pe.eventType, metrics.StatusError).Inc()
			}
			return fmt.Errorf("failed to write event batch to database: %w", err)
		}

		// Exponential backoff capped at 1 second
		backoff := int(50*time.Millisecond) * retry * retry
		dur = time.Duration(min(backoff, int(time.Second)))
		time.Sleep(dur)
	}

	return nil // unreachable
}

// Sends periodic pings to keep the connection alive
func (c *Consumer) pingHandler(ctx context.Context, con *websocket.Conn) {
	ticker := time.NewTicker(15 * time.Second)
	defer ticker.Stop()

	failures := 0
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			err := con.WriteControl(websocket.PingMessage, []byte{}, time.Now().Add(15*time.Second))
			if err == nil {
				failures = 0
				continue
			}

			c.log.Warn("failed to ping upstream", "error", err)
			failures++
			if failures >= 4 {
				c.log.Error("too many ping failures, closing connection")
				if err := con.Close(); err != nil {
					c.log.Error("failed to close websocket connection", "error", err)
				}
				return
			}
		}
	}
}
