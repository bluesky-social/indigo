package main

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/bluesky-social/indigo/atproto/syntax"

	"github.com/bluesky-social/jetstream/pkg/client"
	"github.com/bluesky-social/jetstream/pkg/client/schedulers/sequential"
	"github.com/bluesky-social/jetstream/pkg/models"
)

func handleEvent(ctx context.Context, event *models.Event) error {
	if event.Commit != nil && (event.Commit.Operation == models.CommitOperationCreate || event.Commit.Operation == models.CommitOperationUpdate) {
		if event.Commit.Collection != "com.atproto.lexicon.schema" {
			return nil
		}
		nsid, err := syntax.ParseNSID(event.Commit.RKey)
		if err != nil {
			return fmt.Errorf("invalid NSID in lexicon record: %s", event.Commit.RKey)
		}
		// TODO: enqueue NSID to be crawled/verified
		_ = nsid
	}
	return nil
}

func blah() error {

	logger := slog.Default()

	jetstreamHost := "wss://jetstream2.us-west.bsky.network/subscribe"
	cfg := client.DefaultClientConfig()
	cfg.Compress = true
	cfg.WebsocketURL = jetstreamHost
	cfg.WantedCollections = []string{"com.atproto.lexicon.schema"}

	sched := sequential.NewScheduler("lexidex", logger, handleEvent)

	jc, err := client.NewClient(cfg, logger, sched)
	if err != nil {
		return err
	}
	// TODO: finish this part
	_ = jc
	return nil
}
