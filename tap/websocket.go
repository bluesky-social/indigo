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

// @DOCSTRING
type Websocket struct {
	addr               string
	connectTimeout     time.Duration
	readTimeout        time.Duration
	logger             *slog.Logger
	maxConsecutiveErrs int
	handler            WebsocketHandlerFunc
}

// @DOCSTRING
type WebsocketOption func(*Websocket)

// @DOCSTRING
func WithConnectTimeout(timeout time.Duration) func(*Websocket) {
	return func(ws *Websocket) {
		ws.connectTimeout = timeout
	}
}

// @DOCSTRING
func WithLogger(logger *slog.Logger) func(*Websocket) {
	return func(ws *Websocket) {
		ws.logger = logger

		if ws.logger == nil {
			// write to io.Discard if a nil logger is passed
			ws.logger = slog.New(slog.NewTextHandler(io.Discard, nil))
		}
	}
}

// @DOCSTRING
func WithMaxConsecutiveErrors(numErrs int) func(*Websocket) {
	return func(ws *Websocket) {
		ws.maxConsecutiveErrs = numErrs
	}
}

// @DOCSTRING
type WebsocketHandlerFunc func(context.Context, *Event)

// @DOCSTRING
func NewWebsocket(ctx context.Context, addr string, handler WebsocketHandlerFunc, opts ...WebsocketOption) (*Websocket, error) {
	// validate user args
	u, err := url.Parse(addr)
	if err != nil {
		return nil, fmt.Errorf("failed to parse websocket url %q: %w", err)
	}

	switch u.Scheme {
	case "ws://", "wss://":
		// ok
	default:
		return nil, fmt.Errorf("invalid websocket protocol scheme: wanted ws:// or wss://, got %q", u.Scheme)
	}

	if handler == nil {
		return nil, fmt.Errorf("a websocket message handler func is required")
	}

	ws := &Websocket{
		addr:               addr,
		connectTimeout:     15 * time.Second,
		readTimeout:        time.Minute,
		logger:             slog.Default().WithGroup("tap"),
		maxConsecutiveErrs: 5,
		handler:            handler,
	}

	for _, opt := range opts {
		opt(ws)
	}

	return ws, nil
}

// @DOCSTRING
func (ws *Websocket) Run(ctx context.Context) error {
	const initialBackoff = time.Second

	errCount := 0
	backoff := initialBackoff

	for {
		select {
		case <-ctx.Done():
			ws.logger.Debug("websocket ingester shutting down")
			return nil
		default:
		}

		err := ws.runOnce(ctx)
		if errors.Is(err, context.Canceled) {
			ws.logger.Debug("websocket ingester shutting down")
			return nil
		}

		if err == nil {
			errCount = 0
			backoff = initialBackoff
			ws.logger.Debug("websocket connection closed normally, reconnecting")
			continue
		}

		errCount++
		ws.logger.Error("websocket connection failed",
			"err", err,
			"consecutive_errors", errCount,
		)

		if errCount >= ws.maxConsecutiveErrs {
			return fmt.Errorf("webcosket connection failed %d consecutive times: %w", errCount, err)
		}

		ws.logger.Warn("retrying websocket connection", "consecutive_errors", errCount)

		select {
		case <-ctx.Done():
			// shutdown received during a backoff sleep; nothing to do
			return nil
		case <-time.After(backoff):
		}

		// wait exponentially, up to a limit of 10 seconds
		backoff = min(backoff*2, 10*time.Second)
	}
}

func (ws *Websocket) close(conn *websocket.Conn) {
	if err := conn.Close(); err != nil {
		ws.logger.Error("failed to close websocket connection", "err", err)
	}
}

func (ws *Websocket) runOnce(ctx context.Context) error {
	dialer := websocket.Dialer{HandshakeTimeout: ws.connectTimeout}
	conn, _, err := dialer.DialContext(ctx, ws.addr, nil)
	if err != nil {
		return fmt.Errorf("failed to connect to websocket at %q: %w", ws.addr, err)
	}
	defer ws.close(conn)

	ws.logger.Debug("connected to websocket", "addr", ws.addr)

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

		var event Event
		if err := json.Unmarshal(buf, &event); err != nil {
			ws.logger.Error("failed to unmarshal event json", "err", err)
			continue
		}

		ws.handler(ctx, &event)
	}
}
