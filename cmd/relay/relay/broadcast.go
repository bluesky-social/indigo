package relay

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"sync"
	"time"

	"github.com/bluesky-social/indigo/cmd/relay/stream"

	"github.com/gorilla/websocket"
	promclient "github.com/prometheus/client_golang/prometheus"
	dto "github.com/prometheus/client_model/go"
)

type SocketConsumer struct {
	UserAgent   string
	RemoteAddr  string
	ConnectedAt time.Time
	EventsSent  promclient.Counter
}

func (r *Relay) registerConsumer(c *SocketConsumer) uint64 {
	r.consumersLk.Lock()
	defer r.consumersLk.Unlock()

	id := r.nextConsumerID
	r.nextConsumerID++

	r.consumers[id] = c

	return id
}

func (r *Relay) cleanupConsumer(id uint64) {
	r.consumersLk.Lock()
	defer r.consumersLk.Unlock()

	c := r.consumers[id]

	var m = &dto.Metric{}
	if err := c.EventsSent.Write(m); err != nil {
		r.Logger.Error("failed to get sent counter", "err", err)
	}

	r.Logger.Info("consumer disconnected",
		"consumer_id", id,
		"remote_addr", c.RemoteAddr,
		"user_agent", c.UserAgent,
		"events_sent", m.Counter.GetValue())

	delete(r.consumers, id)
}

var wsUpgrader = websocket.Upgrader{
	ReadBufferSize:  10 << 10,
	WriteBufferSize: 10 << 10,
}

// Main HTTP request handler for clients connecting to the firehose (com.atproto.sync.subscribeRepos)
func (r *Relay) HandleSubscribeRepos(resp http.ResponseWriter, req *http.Request, since *int64, realIP string) error {

	ctx, cancel := context.WithCancel(req.Context())
	defer cancel()

	conn, err := wsUpgrader.Upgrade(resp, req, resp.Header())
	if err != nil {
		return fmt.Errorf("upgrading websocket: %w", err)
	}

	defer func() {
		_ = conn.Close()
	}()

	lastWriteLk := sync.Mutex{}
	lastWrite := time.Now()

	// Start a goroutine to ping the client every 30 seconds to check if it's
	// still alive. If the client doesn't respond to a ping within 5 seconds,
	// we'll close the connection and teardown the consumer.
	go func() {
		ticker := time.NewTicker(30 * time.Second)
		defer ticker.Stop()

		for {
			select {
			case <-ticker.C:
				lastWriteLk.Lock()
				lw := lastWrite
				lastWriteLk.Unlock()

				if time.Since(lw) < 30*time.Second {
					continue
				}

				if err := conn.WriteControl(websocket.PingMessage, []byte{}, time.Now().Add(5*time.Second)); err != nil {
					r.Logger.Warn("failed to ping client", "err", err)
					cancel()
					return
				}
			case <-ctx.Done():
				return
			}
		}
	}()

	conn.SetPingHandler(func(message string) error {
		err := conn.WriteControl(websocket.PongMessage, []byte(message), time.Now().Add(time.Second*60))
		if err == websocket.ErrCloseSent {
			return nil
		} else if e, ok := err.(net.Error); ok && e.Temporary() {
			// TODO: use of e.Temporary() here seems suspicious? maybe related to consumer failures?
			return nil
		}
		return err
	})

	// Start a goroutine to read messages from the client and discard them.
	go func() {
		for {
			_, _, err := conn.ReadMessage()
			if err != nil {
				r.Logger.Warn("failed to read message from client", "err", err)
				cancel()
				return
			}
		}
	}()

	ident := realIP + "-" + req.UserAgent()

	evts, cleanup, err := r.Events.Subscribe(ctx, ident, func(evt *stream.XRPCStreamEvent) bool { return true }, since)
	if err != nil {
		return err
	}
	defer cleanup()

	// Keep track of the consumer for metrics and admin endpoints
	consumer := SocketConsumer{
		RemoteAddr:  realIP,
		UserAgent:   req.UserAgent(),
		ConnectedAt: time.Now(),
	}
	sentCounter := eventsSentCounter.WithLabelValues(consumer.RemoteAddr, consumer.UserAgent)
	consumer.EventsSent = sentCounter

	consumerID := r.registerConsumer(&consumer)
	defer r.cleanupConsumer(consumerID)

	logger := r.Logger.With(
		"consumer_id", consumerID,
		"remote_addr", consumer.RemoteAddr,
		"user_agent", consumer.UserAgent,
	)

	logger.Info("new consumer", "cursor", since)

	for {
		select {
		case evt, ok := <-evts:
			if !ok {
				logger.Error("event stream closed unexpectedly")
				return nil
			}

			wc, err := conn.NextWriter(websocket.BinaryMessage)
			if err != nil {
				logger.Error("failed to get next writer", "err", err)
				return err
			}

			if evt.Preserialized != nil {
				_, err = wc.Write(evt.Preserialized)
			} else {
				err = evt.Serialize(wc)
			}
			if err != nil {
				return fmt.Errorf("failed to write event: %w", err)
			}

			if err := wc.Close(); err != nil {
				logger.Warn("failed to flush-close our event write", "err", err)
				return nil
			}

			lastWriteLk.Lock()
			lastWrite = time.Now()
			lastWriteLk.Unlock()
			sentCounter.Inc()
		case <-ctx.Done():
			return nil
		}
	}
}

type ConsumerInfo struct {
	ID             uint64    `json:"id"`
	RemoteAddr     string    `json:"remote_addr"`
	UserAgent      string    `json:"user_agent"`
	EventsConsumed uint64    `json:"events_consumed"`
	ConnectedAt    time.Time `json:"connected_at"`
}

func (r *Relay) ListConsumers() []ConsumerInfo {
	r.consumersLk.RLock()
	defer r.consumersLk.RUnlock()

	info := make([]ConsumerInfo, 0, len(r.consumers))
	for id, c := range r.consumers {
		var m = &dto.Metric{}
		if err := c.EventsSent.Write(m); err != nil {
			continue
		}
		info = append(info, ConsumerInfo{
			ID:             id,
			RemoteAddr:     c.RemoteAddr,
			UserAgent:      c.UserAgent,
			EventsConsumed: uint64(m.Counter.GetValue()),
			ConnectedAt:    c.ConnectedAt,
		})
	}
	return info
}
