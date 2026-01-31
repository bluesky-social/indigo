package websocket

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/bluesky-social/indigo/internal/ticker"
	"github.com/gorilla/websocket"
)

const (
	// TextMessage represents a text data message. The text message payload is
	// interpreted as UTF-8 encoded text data.
	TextMessage = websocket.TextMessage

	// BinaryMessage represents a binary data message.
	BinaryMessage = websocket.BinaryMessage
)

type Client struct {
	ctx       context.Context
	logger    *slog.Logger
	url       string
	userAgent string

	err error

	state func(*Client)

	dialer          websocket.Dialer
	conn            *websocket.Conn
	backoffStrategy backoffStrategy

	message struct {
		typ int
		r   io.Reader
	}
}

func NewClient(ctx context.Context, logger *slog.Logger, url, userAgent string) *Client {
	return &Client{
		ctx:       ctx,
		logger:    logger.With("component", "websocket_client", "url", url),
		url:       url,
		userAgent: userAgent,
		dialer: websocket.Dialer{
			Proxy:            websocket.DefaultDialer.Proxy,
			HandshakeTimeout: 10 * time.Second,
		},
		backoffStrategy: backoffStrategy{
			initial:    1 * time.Second,
			multiplier: 1.5,
			max:        30 * time.Second,
		},
		state: (*Client).connectState,
	}
}

// Next returms true if there is more data to read.
// If next returns false, check the Err() method for any error encountered.
func (c *Client) Next() bool {
	for c.err == nil && c.message.typ == 0 {
		c.state(c)
	}
	return c.err == nil
}

// Err returns any error encountered during reading.
func (c *Client) Err() error {
	return c.err
}

// Close closes any resources held by the client.
func (c *Client) Close() error {
	if c.conn != nil {
		return c.conn.Close()
	}
	return nil
}

// Message returns the current message value, it is analogous to gorilla/websocket.Conn.NextReader.
// Calling Message consumes the current message, so subsequent calls to Message will return nil
func (c *Client) Message() (int, io.Reader) {
	typ, r := c.message.typ, c.message.r
	c.message.typ = 0
	c.message.r = nil
	return typ, r
}

func (c *Client) connectState() {
	attempts := c.backoffStrategy.attempts
	d := c.backoffStrategy.Next()
	if attempts > 0 {
		c.logger.Info("reconnecting", "delay", d, "attempt", attempts)
	}
	sleep(c.ctx, d)
	c.logger.Info("connecting", "attempt", attempts)
	c.conn, _, c.err = c.dialer.DialContext(c.ctx, c.url, http.Header{
		"User-Agent": []string{c.userAgent},
	})
	if c.err != nil {
		c.logger.Warn("connection error", "error", c.err)
		c.err = nil
		return
	}
	c.logger.Info("connected", "attempt", attempts)

	// set a +1 minute initial read deadline, if will be extended by pings/pongs
	if err := c.conn.SetReadDeadline(time.Now().Add(time.Minute)); err != nil {
		c.err = fmt.Errorf("failed to set read deadline: %w", c.err)
		return
	}

	// TODO(dfc) sending pings periodically seems disarm the read deadline. Not sure why.
	go ticker.Periodically(c.ctx, 30*time.Second, func(ctx context.Context) error {
		// TODO(dfc) this is a data race, the whole send a ping without respect to the conn being used elsewhere
		// needs to be revisited
		if c.conn == nil {
			return nil
		}
		c.logger.Info("sending ping")
		return c.conn.WriteControl(websocket.PingMessage, []byte{}, time.Now().Add(10*time.Second))
	})
	// pong handler moves the read deadline forward
	c.conn.SetPongHandler(func(appData string) error {
		c.logger.Info("received pong")
		return c.conn.SetReadDeadline(time.Now().Add(time.Minute))
	})

	// setup ping handler for server -> client pings
	c.conn.SetPingHandler(func(appData string) error {
		c.logger.Info("received ping, sending pong")
		c.err = c.conn.WriteControl(websocket.PongMessage, []byte(appData), time.Now().Add(10*time.Second))
		return c.err
	})

	c.backoffStrategy.Reset()
	c.state = (*Client).readState
}

func (c *Client) readState() {
	c.message.typ, c.message.r, c.err = c.conn.NextReader()
	if c.err != nil {
		c.logger.Error("websocket read error", "error", c.err, "debug", fmt.Sprintf("%T %+v", c.err, c.err))
		if shouldReconnect(c.err) {
			c.logger.Error("websocket connection closed, will reconnect", "error", c.err)
			c.conn.Close()
			c.conn = nil
			c.state = (*Client).connectState
			c.err = nil
		}
		return
	}
}

func shouldReconnect(err error) bool {
	if cerr := new(websocket.CloseError); errors.As(err, &cerr) {
		// other end hung up, try to reconnect
		return true
	}
	// TODO(dfc) gross hack because gorilla/websocket doesn't export its netError type
	if strings.HasSuffix(err.Error(), "i/o timeout") {
		return true
	}
	return false
}

type backoffStrategy struct {
	initial    time.Duration
	multiplier float64
	max        time.Duration
	attempts   int
}

func (b *backoffStrategy) Next() time.Duration {
	// first time, no wait
	if b.attempts == 0 {
		b.attempts++
		return 0
	}

	d := time.Duration(float64(b.initial) * pow(b.multiplier, float64(b.attempts)))
	if d > b.max {
		d = b.max
	}

	b.attempts++
	return d
}

func (b *backoffStrategy) Reset() {
	b.attempts = 0
}

func pow(a, b float64) float64 {
	result := 1.0
	for i := 0; i < int(b); i++ {
		result *= a
	}
	return result
}

// sleep sleeps for the given duration or until the context is done.
func sleep(ctx context.Context, d time.Duration) error {
	t := time.NewTimer(d)
	defer t.Stop()

	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-t.C:
		return nil
	}
}
