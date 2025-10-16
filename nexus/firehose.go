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
	RelayHost   string
	Logger      *slog.Logger
	Parallelism int
	Callbacks   *events.RepoStreamCallbacks
	GetCursor   func(ctx context.Context, relayHost string) (int64, error)
}

func (fc *FirehoseConsumer) Run(ctx context.Context) error {
	scheduler := parallel.NewScheduler(
		fc.Parallelism,
		100,
		fc.RelayHost,
		fc.Callbacks.EventHandler,
	)

	u, err := url.Parse(fc.RelayHost)
	if err != nil {
		return fmt.Errorf("invalid relayHost URI: %w", err)
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

		cursor, err := fc.GetCursor(ctx, fc.RelayHost)
		if err != nil {
			return fmt.Errorf("failed to read cursor: %w", err)
		}

		if cursor > 0 {
			u.RawQuery = fmt.Sprintf("cursor=%d", cursor)
		}
		urlStr := u.String()

		fc.Logger.Info("connecting to firehose", "url", urlStr, "cursor", cursor, "retries", retries)

		dialer := websocket.DefaultDialer
		con, _, err := dialer.DialContext(ctx, urlStr, http.Header{})
		if err != nil {
			fc.Logger.Warn("dialing failed", "err", err, "retries", retries)
			time.Sleep(backoff(retries, 10))
			retries++
			continue
		}

		fc.Logger.Info("connected to firehose")
		retries = 0

		if err := events.HandleRepoStream(ctx, con, scheduler, nil); err != nil {
			fc.Logger.Warn("firehose connection failed", "err", err)
		}
	}
}
