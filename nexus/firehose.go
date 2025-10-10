package main

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"

	"github.com/bluesky-social/indigo/events"
	"github.com/bluesky-social/indigo/events/schedulers/parallel"
	"github.com/gorilla/websocket"
)

type FirehoseConsumer struct {
	RelayHost   string
	Cursor      int64
	Logger      *slog.Logger
	Parallelism int
	Callbacks   *events.RepoStreamCallbacks
}

func (fc *FirehoseConsumer) Run(ctx context.Context) error {
	dialer := websocket.DefaultDialer
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
	if fc.Cursor != 0 {
		u.RawQuery = fmt.Sprintf("cursor=%d", fc.Cursor)
	}
	urlString := u.String()
	fc.Logger.Info("subscribing to firehose", "relayHost", fc.RelayHost, "cursor", fc.Cursor)
	con, _, err := dialer.Dial(urlString, http.Header{})
	if err != nil {
		return fmt.Errorf("subscribing to firehose failed (dialing): %w", err)
	}

	scheduler := parallel.NewScheduler(
		fc.Parallelism,
		100,
		fc.RelayHost,
		fc.Callbacks.EventHandler,
	)
	return events.HandleRepoStream(ctx, con, scheduler, nil)
}
