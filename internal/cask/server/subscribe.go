package server

import (
	"context"
	"fmt"
	"net"
	"strconv"
	"sync"
	"sync/atomic"
	"time"

	"github.com/gorilla/websocket"
	"github.com/labstack/echo/v4"
)

const (
	// Maximum number of events to fetch per FDB read
	eventBatchSize = 100

	// How long to wait before polling FDB again when no new events
	pollInterval = 10 * time.Millisecond

	pingInterval    = 15 * time.Second
	pingPongTimeout = 10 * time.Second
)

// subscriber represents an active subscribeRepos connection
type subscriber struct {
	id          uint64
	remoteAddr  string
	userAgent   string
	connectedAt time.Time
	eventsSent  atomic.Uint64
}

var upgrader = websocket.Upgrader{
	ReadBufferSize:  10 << 10,
	WriteBufferSize: 10 << 10,
}

// handleSubscribeRepos handles the com.atproto.sync.subscribeRepos XRPC endpoint.
// It streams firehose events from FoundationDB to the client over a WebSocket.
func (s *Server) handleSubscribeRepos(c echo.Context) error {
	// Parse optional cursor parameter (upstream sequence number)
	var cursorSeq int64
	hasCursor := false
	if cursorStr := c.QueryParam("cursor"); cursorStr != "" {
		var err error
		cursorSeq, err = strconv.ParseInt(cursorStr, 10, 64)
		if err != nil {
			return echo.NewHTTPError(400, "invalid cursor format")
		}
		hasCursor = true
	}

	ctx, cancel := context.WithCancel(c.Request().Context())
	defer cancel()

	// Upgrade to websocket
	conn, err := upgrader.Upgrade(c.Response(), c.Request(), nil)
	if err != nil {
		return fmt.Errorf("upgrading websocket: %w", err)
	}
	defer conn.Close() //nolint:errcheck

	// Track last write time for ping logic
	lastWriteMu := sync.Mutex{}
	lastWrite := time.Now()

	// Ping goroutine - keeps connection alive and detects dead clients
	go func() {
		ticker := time.NewTicker(pingInterval)
		defer ticker.Stop()

		for {
			select {
			case <-ticker.C:
				lastWriteMu.Lock()
				lw := lastWrite
				lastWriteMu.Unlock()

				// Skip ping if we wrote recently
				if time.Since(lw) < pingInterval {
					continue
				}

				if err := conn.WriteControl(websocket.PingMessage, []byte{}, time.Now().Add(pingPongTimeout)); err != nil {
					s.log.Info("failed to ping client", "error", err)
					cancel()
					return
				}
			case <-ctx.Done():
				return
			}
		}
	}()

	// Handle incoming pings from client
	conn.SetPingHandler(func(message string) error {
		err := conn.WriteControl(websocket.PongMessage, []byte(message), time.Now().Add(pingPongTimeout))
		if err == websocket.ErrCloseSent {
			return nil
		} else if netErr, ok := err.(net.Error); ok && netErr.Timeout() {
			return nil
		}
		return err
	})

	// Read and discard incoming messages (clients shouldn't send anything)
	go func() {
		for {
			_, _, err := conn.ReadMessage()
			if err != nil {
				cancel()
				return
			}
		}
	}()

	// Register subscriber for tracking
	sub := &subscriber{
		remoteAddr:  c.RealIP(),
		userAgent:   c.Request().UserAgent(),
		connectedAt: time.Now(),
	}
	subID := s.registerSubscriber(sub)
	defer s.unregisterSubscriber(subID, sub)

	s.log.Info("new subscriber",
		"remote_addr", sub.remoteAddr,
		"user_agent", sub.userAgent,
		"cursor", cursorSeq,
		"subscriber_id", subID,
	)

	// Internal cursor (versionstamp) for efficient pagination after initial positioning
	var versionstampCursor []byte

	// If client provided a cursor, we need to find events after that sequence
	// Once we find them, we use versionstamp cursor for efficient continuation
	needsInitialSeek := hasCursor

	// Main event streaming loop
	for {
		select {
		case <-ctx.Done():
			return nil
		default:
		}

		var events []*eventWithCursor
		var err error

		if needsInitialSeek {
			// First fetch: find events after the upstream sequence cursor
			var rawEvents []*eventData
			rawEvents, versionstampCursor, err = s.getEventsAfterSeq(ctx, cursorSeq, eventBatchSize)
			if err != nil {
				s.log.Error("failed to read events from FDB", "error", err)
				return err
			}
			for _, e := range rawEvents {
				events = append(events, &eventWithCursor{data: e})
			}
			needsInitialSeek = false
		} else {
			// Subsequent fetches: use versionstamp cursor for efficiency
			var rawEvents []*eventData
			rawEvents, versionstampCursor, err = s.getEventsSince(ctx, versionstampCursor, eventBatchSize)
			if err != nil {
				s.log.Error("failed to read events from FDB", "error", err)
				return err
			}
			for _, e := range rawEvents {
				events = append(events, &eventWithCursor{data: e})
			}
		}

		if len(events) == 0 {
			// No new events, wait before polling again
			select {
			case <-ctx.Done():
				return nil
			case <-time.After(pollInterval):
				continue
			}
		}

		// Stream events to client
		for _, evt := range events {
			wc, err := conn.NextWriter(websocket.BinaryMessage)
			if err != nil {
				s.log.Debug("failed to get writer", "error", err)
				return nil
			}

			// Write the raw CBOR bytes directly - no re-serialization needed
			if _, err := wc.Write(evt.data.rawEvent); err != nil {
				s.log.Debug("failed to write event", "error", err)
				return nil
			}

			if err := wc.Close(); err != nil {
				s.log.Debug("failed to flush event", "error", err)
				return nil
			}

			sub.eventsSent.Add(1)
		}

		lastWriteMu.Lock()
		lastWrite = time.Now()
		lastWriteMu.Unlock()
	}
}

type eventData struct {
	rawEvent    []byte
	upstreamSeq int64
}

type eventWithCursor struct {
	data *eventData
}

func (s *Server) getEventsAfterSeq(ctx context.Context, afterSeq int64, limit int) ([]*eventData, []byte, error) {
	events, cursor, err := s.models.GetEventsAfterSeq(ctx, afterSeq, limit)
	if err != nil {
		return nil, nil, err
	}

	result := make([]*eventData, len(events))
	for i, evt := range events {
		result[i] = &eventData{
			rawEvent:    evt.RawEvent,
			upstreamSeq: evt.UpstreamSeq,
		}
	}
	return result, cursor, nil
}

func (s *Server) getEventsSince(ctx context.Context, cursor []byte, limit int) ([]*eventData, []byte, error) {
	events, nextCursor, err := s.models.GetEventsSince(ctx, cursor, limit)
	if err != nil {
		return nil, nil, err
	}

	result := make([]*eventData, len(events))
	for i, evt := range events {
		result[i] = &eventData{
			rawEvent:    evt.RawEvent,
			upstreamSeq: evt.UpstreamSeq,
		}
	}
	return result, nextCursor, nil
}

func (s *Server) registerSubscriber(sub *subscriber) uint64 {
	s.subscribersMu.Lock()
	defer s.subscribersMu.Unlock()

	id := s.nextSubscriberID
	s.nextSubscriberID++
	sub.id = id

	if s.subscribers == nil {
		s.subscribers = make(map[uint64]*subscriber)
	}
	s.subscribers[id] = sub

	return id
}

func (s *Server) unregisterSubscriber(id uint64, sub *subscriber) {
	s.subscribersMu.Lock()
	defer s.subscribersMu.Unlock()

	s.log.Info("subscriber disconnected",
		"subscriber_id", id,
		"remote_addr", sub.remoteAddr,
		"user_agent", sub.userAgent,
		"events_sent", sub.eventsSent.Load(),
		"connected_duration", time.Since(sub.connectedAt),
	)

	delete(s.subscribers, id)
}
