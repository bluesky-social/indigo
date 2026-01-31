package server

import (
	"context"
	"fmt"
	"maps"
	"net"
	"strconv"
	"sync"
	"sync/atomic"
	"time"

	"github.com/bluesky-social/indigo/internal/cask/metrics"
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
}

var upgrader = websocket.Upgrader{
	ReadBufferSize:    10 << 10,
	WriteBufferSize:   10 << 10,
	EnableCompression: false,
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

	// Ping goroutine - keeps connection alive and detects dead clients
	go func() {
		ticker := time.NewTicker(pingInterval)
		defer ticker.Stop()

		for {
			select {
			case <-ticker.C:
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
		conn:        conn,
		remoteAddr:  c.RealIP(),
		userAgent:   c.Request().UserAgent(),
		connectedAt: time.Now(),
	}
	s.registerSubscriber(sub)
	defer s.unregisterSubscriber(sub)

	s.log.Info("new subscriber",
		"remote_addr", sub.remoteAddr,
		"user_agent", sub.userAgent,
		"cursor", cursorSeq,
		"subscriber_id", sub.id,
	)

	// Convert the upstream sequence cursor to a versionstamp cursor for efficient streaming.
	// If no cursor was provided, start from the "tip" (only receive new events).
	// This matches the behavior of the upstream relay's subscribeRepos.
	var versionstampCursor []byte
	if hasCursor {
		// Convert upstream seq cursor to versionstamp cursor (single key lookup)
		var err error
		versionstampCursor, err = s.getVersionstampForSeq(ctx, cursorSeq)
		if err != nil {
			s.log.Error("failed to lookup cursor", "error", err)
			return err
		}
		// Note: versionstampCursor may be nil if cursor is older than all events,
		// which means we'll start from the beginning of the events subspace.
	} else {
		// No cursor provided: start from the "tip" (latest event)
		// Get the latest versionstamp so we only receive new events from now on
		var err error
		versionstampCursor, err = s.models.GetLatestVersionstamp(ctx)
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

		// If there are no new events, wait before polling again
		if len(rawEvents) == 0 {
			select {
			case <-ctx.Done():
				return nil
			case <-time.After(pollInterval):
			}
			continue
		}

		// Send events to the client
		for _, evt := range rawEvents {
			if err := s.writeEvent(conn, evt); err != nil {
				s.log.Debug("failed to write event", "error", err)
				return nil
			}

			sub.eventsSent.Add(1)
			metrics.EventsSentTotal.WithLabelValues(sub.remoteAddr, sub.userAgent).Inc()
		}

		// Update cursor for next iteration
		versionstampCursor = nextCursor
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

func (s *Server) getVersionstampForSeq(ctx context.Context, seq int64) ([]byte, error) {
	return s.models.GetVersionstampForSeq(ctx, seq)
}

func (s *Server) getEventsSince(ctx context.Context, cursor []byte, limit int) ([]*eventData, []byte, error) {
	events, nextCursor, err := s.models.GetEventsSince(ctx, cursor, limit)
	if err != nil {
		return nil, nil, err
	}

	res := make([]*eventData, 0, len(events))
	for _, evt := range events {
		res = append(res, &eventData{
			rawEvent:    evt.RawEvent,
			upstreamSeq: evt.UpstreamSeq,
		})
	}

	return res, nextCursor, nil
}

// Records the subscriber in the server, and sets its ID on the passed object
func (s *Server) registerSubscriber(sub *subscriber) {
	s.subscribersMu.Lock()
	defer s.subscribersMu.Unlock()

	id := s.nextSubscriberID
	s.nextSubscriberID++
	sub.id = id

	if s.subscribers == nil {
		s.subscribers = make(map[uint64]*subscriber)
	}
	s.subscribers[id] = sub

	// Update metrics
	metrics.ActiveSubscribers.Inc()
	metrics.SubscriberConnections.Inc()
}

func (s *Server) unregisterSubscriber(sub *subscriber) {
	s.log.Info("subscriber disconnected",
		"subscriber_id", sub.id,
		"remote_addr", sub.remoteAddr,
		"user_agent", sub.userAgent,
		"events_sent", sub.eventsSent.Load(),
		"connected_duration", time.Since(sub.connectedAt).String(),
	)

	s.subscribersMu.Lock()
	delete(s.subscribers, sub.id)
	s.subscribersMu.Unlock()

	metrics.ActiveSubscribers.Dec()
}

func (s *Server) closeAllSubscribers() {
	s.subscribersMu.Lock()
	subs := map[uint64]*subscriber{}
	maps.Copy(subs, s.subscribers)
	s.subscribersMu.Unlock()

	s.log.Info("sending close frames to clients", "count", len(subs))

	var wg sync.WaitGroup
	for _, sub := range subs {
		wg.Go(func() {
			closeMsg := websocket.FormatCloseMessage(websocket.CloseGoingAway, "server shutting down")
			_ = sub.conn.WriteControl(websocket.CloseMessage, closeMsg, time.Now().Add(time.Second))
			_ = sub.conn.Close()
		})
	}
	wg.Wait()

	s.log.Info("all close frames sent to clients", "count", len(subs))
}
