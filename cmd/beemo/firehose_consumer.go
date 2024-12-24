package main

import (
	"bytes"
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"

	comatproto "github.com/bluesky-social/indigo/api/atproto"
	appbsky "github.com/bluesky-social/indigo/api/bsky"
	"github.com/bluesky-social/indigo/atproto/syntax"
	"github.com/bluesky-social/indigo/events/schedulers/parallel"
	lexutil "github.com/bluesky-social/indigo/lex/util"

	"github.com/bluesky-social/indigo/events"
	"github.com/bluesky-social/indigo/repo"
	"github.com/bluesky-social/indigo/repomgr"
	"github.com/carlmjohnson/versioninfo"
	"github.com/gorilla/websocket"
)

func RunFirehoseConsumer(ctx context.Context, logger *slog.Logger, relayHost string, postCallback func(context.Context, syntax.DID, syntax.RecordKey, appbsky.FeedPost) error) error {

	dialer := websocket.DefaultDialer
	u, err := url.Parse(relayHost)
	if err != nil {
		return fmt.Errorf("invalid relayHost URI: %w", err)
	}
	// always continue at the current cursor offset (don't provide cursor query param)
	u.Path = "xrpc/com.atproto.sync.subscribeRepos"
	logger.Info("subscribing to repo event stream", "upstream", relayHost)
	con, _, err := dialer.Dial(u.String(), http.Header{
		"User-Agent": []string{fmt.Sprintf("beemo/%s", versioninfo.Short())},
	})
	if err != nil {
		return fmt.Errorf("subscribing to firehose failed (dialing): %w", err)
	}

	rsc := &events.RepoStreamCallbacks{
		RepoCommit: func(evt *comatproto.SyncSubscribeRepos_Commit) error {
			return HandleRepoCommit(ctx, logger, evt, postCallback)
		},
		// NOTE: could add other callbacks as needed
	}

	var scheduler events.Scheduler
	// use parallel scheduler
	parallelism := 4
	scheduler = parallel.NewScheduler(
		parallelism,
		1000,
		relayHost,
		rsc.EventHandler,
	)
	logger.Info("beemo firehose scheduler configured", "scheduler", "parallel", "workers", parallelism)

	return events.HandleRepoStream(ctx, con, scheduler, logger)
}

// NOTE: for now, this function basically never errors, just logs and returns nil. Should think through error processing better.
func HandleRepoCommit(ctx context.Context, logger *slog.Logger, evt *comatproto.SyncSubscribeRepos_Commit, postCallback func(context.Context, syntax.DID, syntax.RecordKey, appbsky.FeedPost) error) error {

	logger = logger.With("event", "commit", "did", evt.Repo, "rev", evt.Rev, "seq", evt.Seq)
	logger.Debug("received commit event")

	if evt.TooBig {
		logger.Warn("skipping tooBig events for now")
		return nil
	}

	did, err := syntax.ParseDID(evt.Repo)
	if err != nil {
		logger.Error("bad DID syntax in event", "err", err)
		return nil
	}

	rr, err := repo.ReadRepoFromCar(ctx, bytes.NewReader(evt.Blocks))
	if err != nil {
		logger.Error("failed to read repo from car", "err", err)
		return nil
	}

	for _, op := range evt.Ops {
		logger = logger.With("eventKind", op.Action, "path", op.Path)
		collection, rkey, err := syntax.ParseRepoPath(op.Path)
		if err != nil {
			logger.Error("invalid path in repo op")
			return nil
		}

		ek := repomgr.EventKind(op.Action)
		switch ek {
		case repomgr.EvtKindCreateRecord, repomgr.EvtKindUpdateRecord:
			// read the record bytes from blocks, and verify CID
			rc, recordCBOR, err := rr.GetRecordBytes(ctx, op.Path)
			if err != nil {
				logger.Error("reading record from event blocks (CAR)", "err", err)
				continue
			}
			if op.Cid == nil || lexutil.LexLink(rc) != *op.Cid {
				logger.Error("mismatch between commit op CID and record block", "recordCID", rc, "opCID", op.Cid)
				continue
			}

			switch collection {
			case "app.bsky.feed.post":
				var post appbsky.FeedPost
				if err := post.UnmarshalCBOR(bytes.NewReader(*recordCBOR)); err != nil {
					logger.Error("failed to parse app.bsky.feed.post record", "err", err)
					continue
				}
				if err := postCallback(ctx, did, rkey, post); err != nil {
					logger.Error("failed to process post record", "err", err)
					continue
				}
			}

		default:
			// ignore other events
		}
	}

	return nil
}
