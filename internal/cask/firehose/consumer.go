package firehose

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
	"github.com/bluesky-social/indigo/internal/cask/models"
	"github.com/bluesky-social/indigo/pkg/prototypes"
	"github.com/gorilla/websocket"
	"google.golang.org/protobuf/types/known/timestamppb"
)

type Consumer struct {
	log    *slog.Logger
	models *models.Models
	host   string
}

func NewConsumer(log *slog.Logger, models *models.Models, host string) *Consumer {
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

// handleConnection reads events from the websocket connection and stores them.
func (c *Consumer) handleConnection(ctx context.Context, con *websocket.Conn) error {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	// Start ping handler goroutine
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

	// Main read loop
	for {
		select {
		case <-ctx.Done():
			c.log.Info("firehose consumer stopped due to normal context cancellation")
			return nil
		default:
		}

		if err := c.readAndStoreEvent(ctx, con); err != nil {
			return err
		}
	}
}

// readAndStoreEvent reads a single event from the websocket and stores it in the backing database
func (c *Consumer) readAndStoreEvent(ctx context.Context, con *websocket.Conn) error {
	mt, rawReader, err := con.NextReader()
	if err != nil {
		return fmt.Errorf("websocket read error: %w", err)
	}

	if mt != websocket.BinaryMessage {
		return fmt.Errorf("expected binary message, got %d", mt)
	}

	// read the entire message into a buffer so we can store the raw bytes
	var buf bytes.Buffer
	if _, err := io.Copy(&buf, rawReader); err != nil {
		return fmt.Errorf("failed to read message: %w", err)
	}
	rawEvent := buf.Bytes()

	// parse just the header to extract event type and sequence
	reader := bytes.NewReader(rawEvent)
	var header events.EventHeader
	if err := header.UnmarshalCBOR(reader); err != nil {
		return fmt.Errorf("failed to parse event header: %w", err)
	}

	// extract sequence number from the event body (if it's a message)
	seq := int64(0)
	eventType := ""
	switch header.Op {
	case events.EvtKindMessage:
		eventType = header.MsgType
		seq = c.extractSequenceNumber(rawEvent, header.MsgType)
	case events.EvtKindErrorFrame:
		eventType = "error"
	}

	event := &prototypes.FirehoseEvent{
		UpstreamSeq: seq,
		RawEvent:    rawEvent,
		ReceivedAt:  timestamppb.Now(),
		EventType:   eventType,
	}

	const retries = 5
	for retry := range retries {
		err := c.models.WriteEvent(ctx, event)
		if err == nil {
			break
		}
		if retry == retries-1 {
			return fmt.Errorf("failed to write event to database: %w", err)
		}

		// exponential backoff
		backoff := int(50*time.Millisecond) * retry * retry
		dur := max(backoff, int(2*time.Second))
		time.Sleep(time.Duration(dur))
	}

	return nil
}

func (c *Consumer) extractSequenceNumber(rawEvent []byte, msgType string) int64 {
	reader := bytes.NewReader(rawEvent)

	// skip the header
	var header events.EventHeader
	if err := header.UnmarshalCBOR(reader); err != nil {
		return 0
	}

	// parse based on message type to extract seq
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
