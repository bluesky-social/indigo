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
	conn        *websocket.Conn
	remoteAddr  string
	userAgent   string
	connectedAt time.Time
	eventsSent  atomic.Uint64
	done        chan struct{} // closed when handler exits
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

	// Register subscriber for tracking and graceful shutdown
	sub := &subscriber{
		conn:        conn,
		remoteAddr:  c.RealIP(),
		userAgent:   c.Request().UserAgent(),
		connectedAt: time.Now(),
		done:        make(chan struct{}),
	}
	subID := s.registerSubscriber(sub)
	defer func() {
		// Unregister first, then signal done. This ensures that when
		// closeAllSubscribers receives on the done channel, the subscriber
		// has already been removed from the map.
		s.unregisterSubscriber(subID, sub)
		close(sub.done)
	}()

	s.log.Info("new subscriber",
		"remote_addr", sub.remoteAddr,
		"user_agent", sub.userAgent,
		"cursor", cursorSeq,
		"subscriber_id", subID,
	)

	// Internal cursor (versionstamp) for efficient pagination after initial positioning
	var versionstampCursor []byte

	// If client provided a cursor, we need to find events after that sequence.
	// If no cursor was provided, start from the "tip" (only receive new events).
	// This matches the behavior of the upstream relay's subscribeRepos.
	if hasCursor {
		// First fetch: find events after the upstream sequence cursor
		var rawEvents []*eventData
		var err error
		rawEvents, versionstampCursor, err = s.getEventsAfterSeq(ctx, cursorSeq, eventBatchSize)
		if err != nil {
			s.log.Error("failed to read events from FDB", "error", err)
			return err
		}

		// Stream any historical events to the client
		for _, evt := range rawEvents {
			if err := s.writeEvent(conn, evt); err != nil {
				s.log.Debug("failed to write event", "error", err)
				return nil
			}
			sub.eventsSent.Add(1)
		}

		lastWriteMu.Lock()
		lastWrite = time.Now()
		lastWriteMu.Unlock()
	} else {
		// No cursor provided: start from the "tip" (latest event)
		// Get the latest versionstamp so we only receive new events from now on
		var err error
		versionstampCursor, err = s.getLatestVersionstamp(ctx)
		if err != nil {
			s.log.Error("failed to get latest versionstamp", "error", err)
			return err
		}
	}

	// Main event streaming loop - poll for new events
	for {
		select {
		case <-ctx.Done():
			return nil
		default:
		}

		// Fetch events after the current versionstamp cursor
		rawEvents, nextCursor, err := s.getEventsSince(ctx, versionstampCursor, eventBatchSize)
		if err != nil {
			s.log.Error("failed to read events from FDB", "error", err)
			return err
		}

		if len(rawEvents) == 0 {
			// No new events, wait before polling again
			select {
			case <-ctx.Done():
				return nil
			case <-time.After(pollInterval):
				continue
			}
		}

		// Update cursor for next iteration
		versionstampCursor = nextCursor

		// Stream events to client
		for _, evt := range rawEvents {
			if err := s.writeEvent(conn, evt); err != nil {
				s.log.Debug("failed to write event", "error", err)
				return nil
			}
			sub.eventsSent.Add(1)
		}

		lastWriteMu.Lock()
		lastWrite = time.Now()
		lastWriteMu.Unlock()
	}
}

// writeEvent writes a single event to the WebSocket connection
func (s *Server) writeEvent(conn *websocket.Conn, evt *eventData) error {
	wc, err := conn.NextWriter(websocket.BinaryMessage)
	if err != nil {
		return err
	}

	// Write the raw CBOR bytes directly - no re-serialization needed
	if _, err := wc.Write(evt.rawEvent); err != nil {
		return err
	}

	return wc.Close()
}

type eventData struct {
	rawEvent    []byte
	upstreamSeq int64
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

func (s *Server) getLatestVersionstamp(ctx context.Context) ([]byte, error) {
	return s.models.GetLatestVersionstamp(ctx)
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

// closeAllSubscribers gracefully closes all active subscriber connections.
// It sends WebSocket close frames in parallel and waits for handlers to exit.
func (s *Server) closeAllSubscribers(timeout time.Duration) {
	// Get snapshot of subscribers while holding lock
	s.subscribersMu.Lock()
	subs := make([]*subscriber, 0, len(s.subscribers))
	for _, sub := range s.subscribers {
		subs = append(subs, sub)
	}
	s.subscribersMu.Unlock()

	if len(subs) == 0 {
		return
	}

	s.log.Info("closing subscriber connections", "count", len(subs))

	// Send close frames to all subscribers in parallel, ignoring errors
	var wg sync.WaitGroup
	for _, sub := range subs {
		wg.Add(1)
		go func(sub *subscriber) {
			defer wg.Done()
			closeMsg := websocket.FormatCloseMessage(websocket.CloseGoingAway, "server shutting down")
			_ = sub.conn.WriteControl(websocket.CloseMessage, closeMsg, time.Now().Add(time.Second)) //nolint:errcheck
		}(sub)
	}
	wg.Wait()

	// Wait for all handlers to exit with timeout
	done := make(chan struct{})
	go func() {
		for _, sub := range subs {
			<-sub.done
		}
		close(done)
	}()

	select {
	case <-done:
		s.log.Info("all subscribers disconnected gracefully")
	case <-time.After(timeout):
		s.log.Warn("timeout waiting for subscribers to disconnect, forcing close")
		// Force close any remaining connections in parallel, ignoring errors
		for _, sub := range subs {
			go func(sub *subscriber) {
				_ = sub.conn.Close() //nolint:errcheck
			}(sub)
		}
	}
}
