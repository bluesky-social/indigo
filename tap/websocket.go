package tap

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/url"
	"time"

	"github.com/gorilla/websocket"
)

// Websocket implements a tap consumer that reads via a websocket
type Websocket struct {
	log *slog.Logger

	addr     string
	sendAcks bool
	maxErrs  int

	connectTimeout time.Duration
	readTimeout    time.Duration
	writeTimeout   time.Duration

	handler WebsocketHandlerFunc
}

// Defines an option for the tap websocket consumer
type WebsocketOption func(*Websocket)

// Defines the log/slog logger to use throughout the lifecycle of the websocket
// consumer. Pass nil to disable logging.
func WithLogger(logger *slog.Logger) func(*Websocket) {
	return func(ws *Websocket) {
		ws.log = logger

		if ws.log == nil {
			// write to io.Discard if a nil logger is passed
			ws.log = slog.New(slog.NewTextHandler(io.Discard, nil))
		}
	}
}

// Sets the connect timeout for connecting to the websocket
func WithConnectTimeout(timeout time.Duration) func(*Websocket) {
	return func(ws *Websocket) {
		ws.connectTimeout = timeout
	}
}

// Sets the read timeout for reading data from the websocket
func WithReadTimeout(timeout time.Duration) func(*Websocket) {
	return func(ws *Websocket) {
		ws.readTimeout = timeout
	}
}

// Sets the write timeout for writing data to the websocket
func WithWriteTimeout(timeout time.Duration) func(*Websocket) {
	return func(ws *Websocket) {
		ws.writeTimeout = timeout
	}
}

// Controls how many times the loop will attempt to reconnect to the websocket in a row before giving up
func WithMaxConsecutiveErrors(numErrs int) func(*Websocket) {
	return func(ws *Websocket) {
		ws.maxErrs = numErrs
	}
}

// Turns on message acknowledgements
func WithAcks() func(*Websocket) {
	return func(ws *Websocket) {
		ws.sendAcks = true
	}
}

// Defines an option for the tap websocket consumer
type WebsocketHandlerFunc func(context.Context, *Event)

// Initializes a tap websocket consumer
func NewWebsocket(ctx context.Context, addr string, handler WebsocketHandlerFunc, opts ...WebsocketOption) (*Websocket, error) {
	u, err := url.Parse(addr)
	if err != nil {
		return nil, fmt.Errorf("failed to parse websocket url %q: %w", addr, err)
	}

	switch u.Scheme {
	case "ws://", "wss://": // ok
	default:
		return nil, fmt.Errorf("invalid websocket protocol scheme: wanted ws:// or wss://, got %q", u.Scheme)
	}

	if handler == nil {
		return nil, fmt.Errorf("a websocket message handler func is required")
	}

	ws := &Websocket{
		log: slog.Default().WithGroup("tap"),

		addr:     addr,
		sendAcks: false,
		maxErrs:  10,

		connectTimeout: 30 * time.Second,
		readTimeout:    30 * time.Second,
		writeTimeout:   30 * time.Second,

		handler: handler,
	}

	for _, opt := range opts {
		opt(ws)
	}

	return ws, nil
}

// Connects to and beings the main tap websocket consumer loop
func (ws *Websocket) Run(ctx context.Context) error {
	const initialBackoff = 500 * time.Millisecond

	errCount := 0
	backoff := initialBackoff

	for {
		select {
		case <-ctx.Done():
			ws.log.Debug("websocket ingester shutting down")
			return nil
		default:
		}

		err := ws.runOnce(ctx)
		if errors.Is(err, context.Canceled) {
			ws.log.Debug("websocket ingester shutting down")
			return nil
		}

		if err == nil {
			errCount = 0
			backoff = initialBackoff
			ws.log.Debug("websocket connection closed normally, reconnecting")
			continue
		}

		errCount++
		ws.log.Error("websocket connection failed", "err", err, "consecutive_errors", errCount)

		if errCount >= ws.maxErrs {
			return fmt.Errorf("webcosket connection failed %d consecutive times: %w", errCount, err)
		}

		ws.log.Warn("retrying websocket connection", "consecutive_errors", errCount)

		select {
		case <-ctx.Done():
			// shutdown received during a backoff sleep; nothing to do
			return nil
		case <-time.After(backoff):
		}

		// wait exponentially, up to a limit
		backoff = min(backoff*2, 10*time.Second)
	}
}

func (ws *Websocket) close(conn *websocket.Conn) {
	if err := conn.Close(); err != nil {
		ws.log.Error("failed to close websocket connection", "err", err)
	}
}

func (ws *Websocket) runOnce(ctx context.Context) error {
	dialer := websocket.Dialer{HandshakeTimeout: ws.connectTimeout}
	conn, _, err := dialer.DialContext(ctx, ws.addr, nil)
	if err != nil {
		return fmt.Errorf("failed to connect to websocket at %q: %w", ws.addr, err)
	}
	defer ws.close(conn)

	ws.log.Debug("connected to websocket", "addr", ws.addr)

	go func() {
		<-ctx.Done()
		ws.close(conn)
	}()

	for {
		if err := conn.SetReadDeadline(time.Now().Add(ws.readTimeout)); err != nil {
			return fmt.Errorf("failed to set websocket read deadline: %w", err)
		}

		_, buf, err := conn.ReadMessage()
		if err != nil {
			if websocket.IsCloseError(err, websocket.CloseNormalClosure, websocket.CloseGoingAway) {
				return nil // normal remote closure
			}

			if ctx.Err() != nil {
				return ctx.Err()
			}

			return fmt.Errorf("failed to read websocket message: %w", err)
		}

		var ev Event
		if err := json.Unmarshal(buf, &ev); err != nil {
			ws.log.Error("failed to unmarshal event json", "err", err)
			continue
		}

		if ws.handler != nil {
			ws.handler(ctx, &ev)
		}

		// ACKs are optional
		if ws.sendAcks {
			if err := ws.ack(conn, &ev); err != nil {
				return fmt.Errorf("failed to acknowledge event: %w", err)
			}
		}
	}
}

func (ws *Websocket) ack(conn *websocket.Conn, ev *Event) error {
	if err := conn.SetWriteDeadline(time.Now().Add(ws.writeTimeout)); err != nil {
		return fmt.Errorf("failed to set write deadline on ack: %w", err)
	}

	if err := conn.WriteJSON(newACKPayload(ev.ID)); err != nil {
		if websocket.IsCloseError(err, websocket.CloseNormalClosure, websocket.CloseGoingAway) {
			return nil // normal remote closure
		}

		return fmt.Errorf("failed to send ack: %w", err)
	}

	return nil
}
