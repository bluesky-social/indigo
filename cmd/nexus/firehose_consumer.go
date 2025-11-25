package main

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"time"

	"github.com/bluesky-social/indigo/events"
	"github.com/bluesky-social/indigo/events/schedulers/parallel"
	"github.com/gorilla/websocket"
)

type FirehoseConsumer struct {
	RelayUrl    string
	Logger      *slog.Logger
	Parallelism int
	Callbacks   *events.RepoStreamCallbacks
	GetCursor   func(ctx context.Context, relayUrl string) (int64, error)
}

// Run connects to the firehose and processes events until context cancellation or error.
func (f *FirehoseConsumer) Run(ctx context.Context) error {

	u, err := url.Parse(f.RelayUrl)
	if err != nil {
		return fmt.Errorf("invalid relay URL: %w", err)
	}
	switch u.Scheme {
	case "http":
		u.Scheme = "ws"
	case "https":
		u.Scheme = "wss"
	}
	u.Path = "xrpc/com.atproto.sync.subscribeRepos"

	var retries int
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		cursor, err := f.GetCursor(ctx, f.RelayUrl)
		if err != nil {
			return fmt.Errorf("failed to read cursor: %w", err)
		}

		if cursor > 0 {
			u.RawQuery = fmt.Sprintf("cursor=%d", cursor)
		}
		urlStr := u.String()

		f.Logger.Info("connecting to firehose", "url", urlStr, "cursor", cursor, "retries", retries)

		dialer := websocket.DefaultDialer
		con, _, err := dialer.DialContext(ctx, urlStr, http.Header{})
		if err != nil {
			f.Logger.Warn("dialing failed", "error", err, "retries", retries)
			time.Sleep(backoff(retries, 10))
			retries++
			continue
		}

		f.Logger.Info("connected to firehose")
		retries = 0

		scheduler := parallel.NewScheduler(
			f.Parallelism,
			100,
			f.RelayUrl,
			f.Callbacks.EventHandler,
		)
		if err := events.HandleRepoStream(ctx, con, scheduler, nil); err != nil {
			f.Logger.Warn("firehose connection failed", "error", err)
		}
	}
}
