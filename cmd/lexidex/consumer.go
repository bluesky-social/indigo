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

func (srv *WebServer) processJetstreamEvent(ctx context.Context, event *models.Event) error {
	if event.Commit != nil {
		if event.Commit.Collection != "com.atproto.lexicon.schema" {
			return nil
		}
		slog.Info("jetstream event", "did", event.Did, "collection", event.Commit.Collection, "rkey", event.Commit.RKey, "rev", event.Commit.Rev)
		nsid, err := syntax.ParseNSID(event.Commit.RKey)
		if err != nil {
			return fmt.Errorf("invalid NSID in lexicon record: %s", event.Commit.RKey)
		}
		if err := CrawlLexicon(ctx, srv.db, nsid, "firehose"); err != nil {
			slog.Error("failed to crawl lexicon", "nsid", nsid, "reason", "firehose")
		}
	}
	return nil
}

func (srv *WebServer) RunConsumer() error {

	logger := slog.Default()

	cfg := client.DefaultClientConfig()
	cfg.Compress = true
	cfg.WebsocketURL = srv.jetstreamHost
	cfg.WantedCollections = []string{"com.atproto.lexicon.schema"}

	sched := sequential.NewScheduler("lexidex", logger, srv.processJetstreamEvent)

	jc, err := client.NewClient(cfg, logger, sched)
	if err != nil {
		return err
	}

	var cursor *int64
	go func() {
		ctx := context.Background()
		logger.Info("starting jetstream consumer", "cursor", cursor)
		jc.ConnectAndRead(ctx, cursor)
	}()
	return nil
}
