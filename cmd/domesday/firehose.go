package main

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"sync/atomic"
	"time"

	comatproto "github.com/bluesky-social/indigo/api/atproto"
	"github.com/bluesky-social/indigo/atproto/syntax"
	"github.com/bluesky-social/indigo/events/schedulers/parallel"

	"github.com/bluesky-social/indigo/events"
	"github.com/carlmjohnson/versioninfo"
	"github.com/gorilla/websocket"
	"github.com/redis/go-redis/v9"
)

var firehoseCursorKey = "domes/firehoseSeq"

func (srv *Server) RunFirehoseConsumer(ctx context.Context, host string, parallelism int) error {

	cur, err := srv.ReadLastCursor(ctx)
	if err != nil {
		return err
	}

	dialer := websocket.DefaultDialer
	u, err := url.Parse(host)
	if err != nil {
		return fmt.Errorf("invalid Host URI: %w", err)
	}
	u.Path = "xrpc/com.atproto.sync.subscribeRepos"
	if cur != 0 {
		u.RawQuery = fmt.Sprintf("cursor=%d", cur)
	}
	srv.logger.Info("subscribing to repo event stream", "upstream", host, "cursor", cur)
	con, _, err := dialer.Dial(u.String(), http.Header{
		"User-Agent": []string{fmt.Sprintf("domesday/%s", versioninfo.Short())},
	})
	if err != nil {
		return fmt.Errorf("subscribing to firehose failed (dialing): %w", err)
	}

	rsc := &events.RepoStreamCallbacks{
		RepoIdentity: func(evt *comatproto.SyncSubscribeRepos_Identity) error {
			atomic.StoreInt64(&srv.lastSeq, evt.Seq)
			ctx := context.Background()
			srv.logger.Info("flushing cache due to #identity firehose event", "did", evt.Did, "handle", evt.Handle, "seq", evt.Seq, "err", err)

			did, err := syntax.ParseDID(evt.Did)
			if err != nil {
				srv.logger.Warn("invalid DID in #identity event", "did", evt.Did, "seq", evt.Seq, "err", err)
				return nil
			}
			if err := srv.dir.PurgeDID(ctx, did); err != nil {
				srv.logger.Error("failed to purge DID from cache", "did", evt.Did, "seq", evt.Seq, "err", err)
				return nil
			}
			if evt.Handle == nil {
				return nil
			}
			handle, err := syntax.ParseHandle(*evt.Handle)
			if err != nil {
				srv.logger.Warn("invalid handle in #identity event", "did", evt.Did, "handle", evt.Handle, "seq", evt.Seq, "err", err)
				return nil
			}
			if err := srv.dir.PurgeHandle(ctx, handle); err != nil {
				srv.logger.Error("failed to purge handle from cache", "did", evt.Did, "handle", evt.Handle, "seq", evt.Seq, "err", err)
				return nil
			}
			return nil
		},
	}

	var scheduler events.Scheduler
	// use a fixed-parallelism scheduler if configured
	scheduler = parallel.NewScheduler(
		parallelism,
		1000,
		host,
		rsc.EventHandler,
	)
	srv.logger.Info("domesday firehose scheduler configured", "scheduler", "parallel", "initial", parallelism)

	return events.HandleRepoStream(ctx, con, scheduler, srv.logger)
}

func (srv *Server) ReadLastCursor(ctx context.Context) (int64, error) {
	// if redis isn't configured, just skip
	if srv.redisClient == nil {
		srv.logger.Info("redis not configured, skipping cursor read")
		return 0, nil
	}

	val, err := srv.redisClient.Get(ctx, firehoseCursorKey).Int64()
	if err == redis.Nil {
		srv.logger.Info("no pre-existing cursor in redis")
		return 0, nil
	} else if err != nil {
		return 0, err
	}
	srv.logger.Info("successfully found prior subscription cursor seq in redis", "seq", val)
	return val, nil
}

func (srv *Server) PersistCursor(ctx context.Context) error {
	// if redis isn't configured, just skip
	if srv.redisClient == nil {
		return nil
	}
	lastSeq := atomic.LoadInt64(&srv.lastSeq)
	if lastSeq <= 0 {
		return nil
	}
	err := srv.redisClient.Set(ctx, firehoseCursorKey, lastSeq, 14*24*time.Hour).Err()
	return err
}

// this method runs in a loop, persisting the current cursor state every 5 seconds
func (srv *Server) RunPersistCursor(ctx context.Context) error {

	// if redis isn't configured, just skip
	if srv.redisClient == nil {
		return nil
	}
	ticker := time.NewTicker(5 * time.Second)
	for {
		select {
		case <-ctx.Done():
			lastSeq := atomic.LoadInt64(&srv.lastSeq)
			if lastSeq >= 1 {
				srv.logger.Info("persisting final cursor seq value", "seq", lastSeq)
				err := srv.PersistCursor(ctx)
				if err != nil {
					srv.logger.Error("failed to persist cursor", "err", err, "seq", lastSeq)
				}
			}
			return nil
		case <-ticker.C:
			lastSeq := atomic.LoadInt64(&srv.lastSeq)
			if lastSeq >= 1 {
				err := srv.PersistCursor(ctx)
				if err != nil {
					srv.logger.Error("failed to persist cursor", "err", err, "seq", lastSeq)
				}
			}
		}
	}
}
