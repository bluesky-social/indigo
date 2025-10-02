package main

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"strings"

	comatproto "github.com/bluesky-social/indigo/api/atproto"
	"github.com/bluesky-social/indigo/events"
	"github.com/bluesky-social/indigo/events/schedulers/parallel"
	"github.com/gorilla/websocket"
)

func (nexus *Nexus) SubscribeFirehose(ctx context.Context) error {
	relayHost := "https://bsky.network"

	dialer := websocket.DefaultDialer
	u, err := url.Parse(relayHost)
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
	// if cmd.IsSet("cursor") {
	// 	u.RawQuery = fmt.Sprintf("cursor=%d", cmd.Int("cursor"))
	// }
	urlString := u.String()
	con, _, err := dialer.Dial(urlString, http.Header{
		// "User-Agent": []string{*userAgent()},
	})
	if err != nil {
		return fmt.Errorf("subscribing to firehose failed (dialing): %w", err)
	}

	rsc := &events.RepoStreamCallbacks{
		RepoCommit: func(evt *comatproto.SyncSubscribeRepos_Commit) error {
			return nexus.handleCommitEvent(ctx, evt)
		},
		RepoSync: func(evt *comatproto.SyncSubscribeRepos_Sync) error {
			return nil
		},
		RepoIdentity: func(evt *comatproto.SyncSubscribeRepos_Identity) error {
			return nil
		},
		RepoAccount: func(evt *comatproto.SyncSubscribeRepos_Account) error {
			return nil
		},
	}

	scheduler := parallel.NewScheduler(
		1,
		100,
		relayHost,
		rsc.EventHandler,
	)
	slog.Info("starting firehose consumer", "relayHost", relayHost)
	err = events.HandleRepoStream(ctx, con, scheduler, nil)

	if err != nil {
		return err
	}
	return events.HandleRepoStream(ctx, con, scheduler, nil)
}

func (nexus *Nexus) handleCommitEvent(ctx context.Context, evt *comatproto.SyncSubscribeRepos_Commit) error {
	if _, exists := nexus.filterDids[evt.Repo]; !exists {
		return nil
	}
	for _, op := range evt.Ops {
		parts := strings.Split(op.Path, "/")
		collection := parts[0]
		rkey := parts[1]
		err := nexus.outbox.Send(&Op{
			DID:        evt.Repo,
			Collection: collection,
			Rkey:       rkey,
		})
		if err != nil {
			return err
		}
	}
	return nil
}
