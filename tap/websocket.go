package tap

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/url"
	"sync"
	"time"

	"github.com/gorilla/websocket"
)

var (
	initialBackoff = 500 * time.Millisecond
)

// A thin error wrapper that indicates to the tap client consumer loop that a message
// should not be retried (i.e. invalid user input that will surely fail again on retry).
type NonRetryableError struct {
	err error
}

func NewNonRetryableError(err error) *NonRetryableError {
	return &NonRetryableError{err: err}
}

func (err *NonRetryableError) Error() string {
	if err.err != nil {
		return err.err.Error()
	}
	return ""
}

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

// Defines an option for the tap websocket consumer. A nil error indicates that an ACK will be sent to tap
// if WithAcks() is provided.
type WebsocketHandlerFunc func(context.Context, *Event) error

// Initializes a tap websocket consumer
func NewWebsocket(addr string, handler WebsocketHandlerFunc, opts ...WebsocketOption) (*Websocket, error) {
	u, err := url.Parse(addr)
	if err != nil {
		return nil, fmt.Errorf("failed to parse websocket url %q: %w", addr, err)
	}

	switch u.Scheme {
	case "ws", "wss": // ok
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
	for errCount := 0; ; {
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
			ws.log.Debug("websocket connection closed normally, reconnecting")
			continue
		}

		errCount++
		ws.log.Error("websocket connection failed", "err", err, "consecutive_errors", errCount)

		if errCount >= ws.maxErrs {
			return fmt.Errorf("websocket connection failed %d consecutive times: %w", errCount, err)
		}

		ws.log.Warn("retrying websocket connection", "consecutive_errors", errCount)
		if sleepMaybeExit(ctx, errCount) {
			return nil
		}

		errCount++
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

	var closeOnce sync.Once
	closeConn := func() {
		closeOnce.Do(func() {
			if err := conn.Close(); err != nil {
				ws.log.Error("failed to close websocket connection", "err", err)
			}
		})
	}
	defer closeConn()

	ws.log.Debug("connected to websocket", "addr", ws.addr)

	go func() {
		<-ctx.Done()
		closeConn()
	}()

	for {
		if done(ctx) {
			return nil
		}

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
			ws.log.Warn("failed to unmarshal event json", "err", err)
			continue
		}

		// indefinitely retry messages that failed to process unless a non-retryable error occurrs
		for errCount := 0; ; errCount++ {
			if done(ctx) {
				break
			}

			err := ws.handler(ctx, &ev)
			if err != nil {
				ws.log.Error("failed to process event", "err", err)
				if sleepMaybeExit(ctx, errCount) {
					return nil
				}

				var nr *NonRetryableError
				if errors.As(err, &nr) {
					ws.log.Error("handled non-retryable error", "id", ev.ID, "err", err)
					break
				}

				continue
			}
		}

		if ws.sendAcks {
			ws.ack(ctx, conn, &ev)
		}
	}
}

// Indefinitely tries acking the message with the tap server
func (ws *Websocket) ack(ctx context.Context, conn *websocket.Conn, ev *Event) {
	for errCount := 0; ; errCount++ {
		if done(ctx) {
			return
		}

		if err := conn.SetWriteDeadline(time.Now().Add(ws.writeTimeout)); err != nil {
			ws.log.Warn("failed to set write deadline on ack", "err", err)
		}

		err := conn.WriteJSON(NewACKPayload(ev.ID))
		if err == nil {
			return
		}

		if websocket.IsCloseError(err, websocket.CloseNormalClosure, websocket.CloseGoingAway) {
			return // normal remote closure
		}

		ws.log.Error("failed to send ack", "err", err)

		if sleepMaybeExit(ctx, errCount) {
			return
		}
	}
}

func done(ctx context.Context) bool {
	select {
	case <-ctx.Done():
		return true
	default:
		return false
	}
}

func sleepMaybeExit(ctx context.Context, errCount int) bool {
	select {
	case <-ctx.Done():
		return true // shutdown received during a backoff sleep means that we're done
	case <-time.After(backoffDuration(errCount)):
		return false
	}
}

func backoffDuration(errCount int) time.Duration {
	multiplier := 1 << errCount
	waitFor := initialBackoff * time.Duration(multiplier)

	return min(waitFor, 10*time.Second)
}
