package consumer

import (
	"bytes"
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"sync/atomic"
	"time"

	comatproto "github.com/bluesky-social/indigo/api/atproto"
	"github.com/bluesky-social/indigo/atproto/syntax"
	"github.com/bluesky-social/indigo/automod"
	"github.com/bluesky-social/indigo/events/schedulers/autoscaling"
	"github.com/bluesky-social/indigo/events/schedulers/parallel"
	lexutil "github.com/bluesky-social/indigo/lex/util"

	"github.com/bluesky-social/indigo/events"
	"github.com/bluesky-social/indigo/repo"
	"github.com/bluesky-social/indigo/repomgr"
	"github.com/carlmjohnson/versioninfo"
	"github.com/gorilla/websocket"
	"github.com/redis/go-redis/v9"
)

// TODO: should probably make this not hepa-specific; or even configurable
var firehoseCursorKey = "hepa/seq"

type FirehoseConsumer struct {
	Parallelism int
	Logger      *slog.Logger
	RedisClient *redis.Client
	Engine      *automod.Engine
	Host        string

	// TODO: prefilter record collections; or predicate function?
	// TODO: enable/disable event types; or predicate function?

	// lastSeq is the most recent event sequence number we've received and begun to handle.
	// This number is periodically persisted to redis, if redis is present.
	// The value is best-effort (the stream handling itself is concurrent, so event numbers may not be monotonic),
	// but nonetheless, you must use atomics when updating or reading this (to avoid data races).
	lastSeq int64
}

func (fc *FirehoseConsumer) Run(ctx context.Context) error {

	if fc.Engine == nil {
		return fmt.Errorf("nil engine")
	}

	cur, err := fc.ReadLastCursor(ctx)
	if err != nil {
		return err
	}

	dialer := websocket.DefaultDialer
	u, err := url.Parse(fc.Host)
	if err != nil {
		return fmt.Errorf("invalid Host URI: %w", err)
	}
	u.Path = "xrpc/com.atproto.sync.subscribeRepos"
	if cur != 0 {
		u.RawQuery = fmt.Sprintf("cursor=%d", cur)
	}
	fc.Logger.Info("subscribing to repo event stream", "upstream", fc.Host, "cursor", cur)
	con, _, err := dialer.Dial(u.String(), http.Header{
		"User-Agent": []string{fmt.Sprintf("hepa/%s", versioninfo.Short())},
	})
	if err != nil {
		return fmt.Errorf("subscribing to firehose failed (dialing): %w", err)
	}

	rsc := &events.RepoStreamCallbacks{
		RepoCommit: func(evt *comatproto.SyncSubscribeRepos_Commit) error {
			atomic.StoreInt64(&fc.lastSeq, evt.Seq)
			return fc.HandleRepoCommit(ctx, evt)
		},
		RepoIdentity: func(evt *comatproto.SyncSubscribeRepos_Identity) error {
			atomic.StoreInt64(&fc.lastSeq, evt.Seq)
			if err := fc.Engine.ProcessIdentityEvent(context.Background(), *evt); err != nil {
				fc.Logger.Error("processing repo identity failed", "did", evt.Did, "seq", evt.Seq, "err", err)
			}
			return nil
		},
		RepoAccount: func(evt *comatproto.SyncSubscribeRepos_Account) error {
			atomic.StoreInt64(&fc.lastSeq, evt.Seq)
			if err := fc.Engine.ProcessAccountEvent(context.Background(), *evt); err != nil {
				fc.Logger.Error("processing repo account failed", "did", evt.Did, "seq", evt.Seq, "err", err)
			}
			return nil
		},
		// NOTE: no longer process #handle events
		// NOTE: no longer process #tombstone events
	}

	var scheduler events.Scheduler
	if fc.Parallelism > 0 {
		// use a fixed-parallelism scheduler if configured
		scheduler = parallel.NewScheduler(
			fc.Parallelism,
			1000,
			fc.Host,
			rsc.EventHandler,
		)
		fc.Logger.Info("hepa scheduler configured", "scheduler", "parallel", "initial", fc.Parallelism)
	} else {
		// otherwise use auto-scaling scheduler
		scaleSettings := autoscaling.DefaultAutoscaleSettings()
		// start at higher parallelism (somewhat arbitrary)
		scaleSettings.Concurrency = 4
		scaleSettings.MaxConcurrency = 200
		scheduler = autoscaling.NewScheduler(scaleSettings, fc.Host, rsc.EventHandler)
		fc.Logger.Info("hepa scheduler configured", "scheduler", "autoscaling", "initial", scaleSettings.Concurrency, "max", scaleSettings.MaxConcurrency)
	}

	return events.HandleRepoStream(ctx, con, scheduler, fc.Logger)
}

// NOTE: for now, this function basically never errors, just logs and returns nil. Should think through error processing better.
func (fc *FirehoseConsumer) HandleRepoCommit(ctx context.Context, evt *comatproto.SyncSubscribeRepos_Commit) error {

	logger := fc.Logger.With("event", "commit", "did", evt.Repo, "rev", evt.Rev, "seq", evt.Seq)
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
			logger.Error("invalid path in repo op", "err", err)
			return nil
		}

		ek := repomgr.EventKind(op.Action)
		switch ek {
		case repomgr.EvtKindCreateRecord, repomgr.EvtKindUpdateRecord:
			// read the record bytes from blocks, and verify CID
			rc, recCBOR, err := rr.GetRecordBytes(ctx, op.Path)
			if err != nil {
				logger.Error("reading record from event blocks (CAR)", "err", err)
				break
			}
			if op.Cid == nil || lexutil.LexLink(rc) != *op.Cid {
				logger.Error("mismatch between commit op CID and record block", "recordCID", rc, "opCID", op.Cid)
				break
			}
			var action string
			switch ek {
			case repomgr.EvtKindCreateRecord:
				action = automod.CreateOp
			case repomgr.EvtKindUpdateRecord:
				action = automod.UpdateOp
			default:
				logger.Error("impossible event kind", "kind", ek)
				break
			}
			recCID := syntax.CID(op.Cid.String())
			op := automod.RecordOp{
				Action:     action,
				DID:        did,
				Collection: collection,
				RecordKey:  rkey,
				CID:        &recCID,
				RecordCBOR: *recCBOR,
			}
			err = fc.Engine.ProcessRecordOp(context.Background(), op)
			if err != nil {
				logger.Error("engine failed to process record", "err", err)
				continue
			}
		case repomgr.EvtKindDeleteRecord:
			op := automod.RecordOp{
				Action:     automod.DeleteOp,
				DID:        did,
				Collection: collection,
				RecordKey:  rkey,
				CID:        nil,
				RecordCBOR: nil,
			}
			err = fc.Engine.ProcessRecordOp(context.Background(), op)
			if err != nil {
				logger.Error("engine failed to process record", "err", err)
				continue
			}
		default:
			// TODO: should this be an error?
		}
	}

	return nil
}

func (fc *FirehoseConsumer) ReadLastCursor(ctx context.Context) (int64, error) {
	// if redis isn't configured, just skip
	if fc.RedisClient == nil {
		fc.Logger.Info("redis not configured, skipping cursor read")
		return 0, nil
	}

	val, err := fc.RedisClient.Get(ctx, firehoseCursorKey).Int64()
	if err == redis.Nil {
		fc.Logger.Info("no pre-existing cursor in redis")
		return 0, nil
	} else if err != nil {
		return 0, err
	}
	fc.Logger.Info("successfully found prior subscription cursor seq in redis", "seq", val)
	return val, nil
}

func (fc *FirehoseConsumer) PersistCursor(ctx context.Context) error {
	// if redis isn't configured, just skip
	if fc.RedisClient == nil {
		return nil
	}
	lastSeq := atomic.LoadInt64(&fc.lastSeq)
	if lastSeq <= 0 {
		return nil
	}
	err := fc.RedisClient.Set(ctx, firehoseCursorKey, lastSeq, 14*24*time.Hour).Err()
	return err
}

// this method runs in a loop, persisting the current cursor state every 5 seconds
func (fc *FirehoseConsumer) RunPersistCursor(ctx context.Context) error {

	// if redis isn't configured, just skip
	if fc.RedisClient == nil {
		return nil
	}
	ticker := time.NewTicker(5 * time.Second)
	for {
		select {
		case <-ctx.Done():
			lastSeq := atomic.LoadInt64(&fc.lastSeq)
			if lastSeq >= 1 {
				fc.Logger.Info("persisting final cursor seq value", "seq", lastSeq)
				err := fc.PersistCursor(ctx)
				if err != nil {
					fc.Logger.Error("failed to persist cursor", "err", err, "seq", lastSeq)
				}
			}
			return nil
		case <-ticker.C:
			lastSeq := atomic.LoadInt64(&fc.lastSeq)
			if lastSeq >= 1 {
				err := fc.PersistCursor(ctx)
				if err != nil {
					fc.Logger.Error("failed to persist cursor", "err", err, "seq", lastSeq)
				}
			}
		}
	}
}
