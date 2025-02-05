package main

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"os"

	comatproto "github.com/bluesky-social/indigo/api/atproto"
	"github.com/bluesky-social/indigo/atproto/repo"
	"github.com/bluesky-social/indigo/events"
	"github.com/bluesky-social/indigo/events/schedulers/parallel"

	"github.com/carlmjohnson/versioninfo"
	"github.com/gorilla/websocket"
	"github.com/urfave/cli/v2"
)

func runVerifyFirehose(cctx *cli.Context) error {
	ctx := context.Background()

	slog.SetDefault(configLogger(cctx, os.Stdout))

	relayHost := cctx.String("relay-host")

	dialer := websocket.DefaultDialer
	u, err := url.Parse(relayHost)
	if err != nil {
		return fmt.Errorf("invalid relayHost URI: %w", err)
	}
	u.Path = "xrpc/com.atproto.sync.subscribeRepos"
	con, _, err := dialer.Dial(u.String(), http.Header{
		"User-Agent": []string{fmt.Sprintf("goat/%s", versioninfo.Short())},
	})
	if err != nil {
		return fmt.Errorf("subscribing to firehose failed (dialing): %w", err)
	}

	rsc := &events.RepoStreamCallbacks{
		RepoCommit: func(evt *comatproto.SyncSubscribeRepos_Commit) error {
			slog.Debug("commit event", "did", evt.Repo, "seq", evt.Seq)
			return handleCommitEvent(ctx, evt)
		},
	}

	scheduler := parallel.NewScheduler(
		1,
		100,
		relayHost,
		rsc.EventHandler,
	)
	slog.Info("starting firehose consumer", "relayHost", relayHost)
	return events.HandleRepoStream(ctx, con, scheduler, nil)
}

func handleCommitEvent(ctx context.Context, evt *comatproto.SyncSubscribeRepos_Commit) error {
	// TODO: just log errors, not fail?
	_, err := repo.VerifyCommitMessage(ctx, evt)
	return err
}
