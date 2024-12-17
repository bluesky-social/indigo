package search

import (
	"bytes"
	"context"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	comatproto "github.com/bluesky-social/indigo/api/atproto"
	bsky "github.com/bluesky-social/indigo/api/bsky"
	"github.com/bluesky-social/indigo/atproto/syntax"
	"github.com/bluesky-social/indigo/backfill"
	"github.com/bluesky-social/indigo/events"
	"github.com/bluesky-social/indigo/events/schedulers/autoscaling"
	lexutil "github.com/bluesky-social/indigo/lex/util"
	"github.com/bluesky-social/indigo/repo"
	typegen "github.com/whyrusleeping/cbor-gen"

	"github.com/carlmjohnson/versioninfo"
	"github.com/gorilla/websocket"
	"github.com/ipfs/go-cid"
)

func (idx *Indexer) getLastCursor() (int64, error) {
	var lastSeq LastSeq
	if err := idx.db.Find(&lastSeq).Error; err != nil {
		return 0, err
	}

	if lastSeq.ID == 0 {
		return 0, idx.db.Create(&lastSeq).Error
	}

	return lastSeq.Seq, nil
}

func (idx *Indexer) updateLastCursor(curs int64) error {
	return idx.db.Model(LastSeq{}).Where("id = 1").Update("seq", curs).Error
}

func (idx *Indexer) RunIndexer(ctx context.Context) error {
	cur, err := idx.getLastCursor()
	if err != nil {
		return fmt.Errorf("get last cursor: %w", err)
	}

	// Start the indexer batch workers
	go idx.runPostIndexer(ctx)
	go idx.runProfileIndexer(ctx)

	err = idx.bfs.LoadJobs(ctx)
	if err != nil {
		return fmt.Errorf("loading backfill jobs: %w", err)
	}
	go idx.bf.Start()

	if idx.enableRepoDiscovery {
		go idx.discoverRepos()
	}

	d := websocket.DefaultDialer
	u, err := url.Parse(idx.relayhost)
	if err != nil {
		return fmt.Errorf("invalid bgshost URI: %w", err)
	}
	u.Path = "xrpc/com.atproto.sync.subscribeRepos"
	if cur != 0 {
		u.RawQuery = fmt.Sprintf("cursor=%d", cur)
	}
	con, _, err := d.Dial(u.String(), http.Header{
		"User-Agent": []string{fmt.Sprintf("palomar/%s", versioninfo.Short())},
	})
	if err != nil {
		return fmt.Errorf("events dial failed: %w", err)
	}

	rsc := &events.RepoStreamCallbacks{
		RepoCommit: func(evt *comatproto.SyncSubscribeRepos_Commit) error {
			ctx := context.Background()
			ctx, span := tracer.Start(ctx, "RepoCommit")
			defer span.End()

			defer func() {
				if evt.Seq%50 == 0 {
					if err := idx.updateLastCursor(evt.Seq); err != nil {
						idx.logger.Error("failed to persist cursor", "err", err)
					}
				}
			}()
			logEvt := idx.logger.With("repo", evt.Repo, "rev", evt.Rev, "seq", evt.Seq)
			if evt.TooBig && evt.Since != nil {
				// TODO: handle this case (instead of return nil)
				logEvt.Error("skipping non-genesis tooBig events for now")
				return nil
			}

			if evt.TooBig {
				if err := idx.processTooBigCommit(ctx, evt); err != nil {
					// TODO: handle this case (instead of return nil)
					logEvt.Error("failed to process tooBig event", "err", err)
					return nil
				}

				return nil
			}

			// Pass events to the backfiller which will process or buffer as needed
			if err := idx.bf.HandleEvent(ctx, evt); err != nil {
				logEvt.Error("failed to handle event", "err", err)
			}

			return nil

		},
		RepoHandle: func(evt *comatproto.SyncSubscribeRepos_Handle) error {
			ctx := context.Background()
			ctx, span := tracer.Start(ctx, "RepoHandle")
			defer span.End()

			did, err := syntax.ParseDID(evt.Did)
			if err != nil {
				idx.logger.Error("bad DID in RepoHandle event", "did", evt.Did, "handle", evt.Handle, "seq", evt.Seq, "err", err)
				return nil
			}
			if err := idx.updateUserHandle(ctx, did, evt.Handle); err != nil {
				// TODO: handle this case (instead of return nil)
				idx.logger.Error("failed to update user handle", "did", evt.Did, "handle", evt.Handle, "seq", evt.Seq, "err", err)
			}
			return nil
		},
	}

	return events.HandleRepoStream(
		ctx, con, autoscaling.NewScheduler(
			autoscaling.DefaultAutoscaleSettings(),
			idx.relayhost,
			rsc.EventHandler,
		),
		idx.logger,
	)
}

func (idx *Indexer) discoverRepos() {
	ctx := context.Background()
	log := idx.logger.With("func", "discoverRepos")
	log.Info("starting repo discovery")

	cursor := ""
	limit := int64(500)

	total := 0
	totalErrored := 0

	for {
		resp, err := comatproto.SyncListRepos(ctx, idx.relayXRPC, cursor, limit)
		if err != nil {
			log.Error("failed to list repos", "err", err)
			time.Sleep(5 * time.Second)
			continue
		}
		log.Info("got repo page", "count", len(resp.Repos), "cursor", resp.Cursor)
		errored := 0
		for _, repo := range resp.Repos {
			_, err := idx.bfs.GetOrCreateJob(ctx, repo.Did, backfill.StateEnqueued)
			if err != nil {
				log.Error("failed to get or create job", "did", repo.Did, "err", err)
				errored++
			}
		}
		log.Info("enqueued repos", "total", len(resp.Repos), "errored", errored)
		totalErrored += errored
		total += len(resp.Repos)
		if resp.Cursor != nil && *resp.Cursor != "" {
			cursor = *resp.Cursor
		} else {
			break
		}
	}

	log.Info("finished repo discovery", "totalJobs", total, "totalErrored", totalErrored)
}

func (idx *Indexer) handleCreateOrUpdate(ctx context.Context, rawDID string, rev string, path string, recB *[]byte, rcid *cid.Cid) error {
	logger := idx.logger.With("func", "handleCreateOrUpdate", "did", rawDID, "rev", rev, "path", path)
	// Since this gets called in a backfill job, we need to check if the path is a post or profile
	if !strings.Contains(path, "app.bsky.feed.post") && !strings.Contains(path, "app.bsky.actor.profile") {
		return nil
	}

	did, err := syntax.ParseDID(rawDID)
	if err != nil {
		return fmt.Errorf("bad DID syntax in event: %w", err)
	}

	// CBOR Unmarshal the record
	recCBOR, err := lexutil.CborDecodeValue(*recB)
	if err != nil {
		return fmt.Errorf("cbor decode: %w", err)
	}

	rec, ok := recCBOR.(typegen.CBORMarshaler)
	if !ok {
		return fmt.Errorf("failed to cast record to CBORMarshaler")
	}

	parts := strings.SplitN(path, "/", 3)
	if len(parts) < 2 {
		logger.Warn("skipping post record with malformed path")
		return nil
	}

	switch rec := rec.(type) {
	case *bsky.FeedPost:
		rkey, err := syntax.ParseTID(parts[1])
		if err != nil {
			logger.Warn("skipping post record with non-TID rkey")
			return nil
		}

		job := PostIndexJob{
			did:    did,
			record: rec,
			rcid:   *rcid,
			rkey:   rkey.String(),
		}

		// Send the job to the bulk indexer
		idx.postQueue <- &job
		postsIndexed.Inc()
	case *bsky.ActorProfile:
		if parts[1] != "self" {
			return nil
		}

		ident, err := idx.dir.LookupDID(ctx, did)
		if err != nil {
			return fmt.Errorf("resolving identity: %w", err)
		}
		if ident == nil {
			return fmt.Errorf("identity not found for did: %s", did.String())
		}

		job := ProfileIndexJob{
			ident:  ident,
			record: rec,
			rcid:   *rcid,
		}

		// Send the job to the bulk indexer
		idx.profileQueue <- &job
		profilesIndexed.Inc()
	default:
	}
	return nil
}

func (idx *Indexer) handleDelete(ctx context.Context, rawDID, rev, path string) error {
	// Since this gets called in a backfill job, we need to check if the path is a post or profile
	if !strings.Contains(path, "app.bsky.feed.post") && !strings.Contains(path, "app.bsky.actor.profile") {
		return nil
	}

	did, err := syntax.ParseDID(rawDID)
	if err != nil {
		return fmt.Errorf("invalid DID in event: %w", err)
	}

	switch {
	// TODO: handle profile deletes, its an edge case, but worth doing still
	case strings.Contains(path, "app.bsky.feed.post"):
		if err := idx.deletePost(ctx, did, path); err != nil {
			return err
		}
		postsDeleted.Inc()
	case strings.Contains(path, "app.bsky.actor.profile"):
		// profilesDeleted.Inc()
	}

	return nil
}

func (idx *Indexer) processTooBigCommit(ctx context.Context, evt *comatproto.SyncSubscribeRepos_Commit) error {
	logger := idx.logger.With("func", "processTooBigCommit", "repo", evt.Repo, "rev", evt.Rev, "seq", evt.Seq)

	repodata, err := comatproto.SyncGetRepo(ctx, idx.relayXRPC, evt.Repo, "")
	if err != nil {
		return err
	}

	r, err := repo.ReadRepoFromCar(ctx, bytes.NewReader(repodata))
	if err != nil {
		return err
	}

	did, err := syntax.ParseDID(evt.Repo)
	if err != nil {
		return fmt.Errorf("bad DID in repo event: %w", err)
	}

	ident, err := idx.dir.LookupDID(ctx, did)
	if err != nil {
		return err
	}
	if ident == nil {
		return fmt.Errorf("identity not found for did: %s", did.String())
	}

	return r.ForEach(ctx, "", func(k string, v cid.Cid) error {
		if strings.HasPrefix(k, "app.bsky.feed.post") || strings.HasPrefix(k, "app.bsky.actor.profile") {
			rcid, rec, err := r.GetRecord(ctx, k)
			if err != nil {
				// TODO: handle this case (instead of return nil)
				idx.logger.Error("failed to get record from repo checkout", "path", k, "err", err)
				return nil
			}

			parts := strings.SplitN(k, "/", 3)
			if len(parts) < 2 {
				logger.Warn("skipping post record with malformed path")
				return nil
			}

			switch rec := rec.(type) {
			case *bsky.FeedPost:
				rkey, err := syntax.ParseTID(parts[1])
				if err != nil {
					logger.Warn("skipping post record with non-TID rkey")
					return nil
				}

				job := PostIndexJob{
					did:    did,
					record: rec,
					rcid:   rcid,
					rkey:   rkey.String(),
				}

				// Send the job to the bulk indexer
				idx.postQueue <- &job
			case *bsky.ActorProfile:
				if parts[1] != "self" {
					return nil
				}

				job := ProfileIndexJob{
					ident:  ident,
					record: rec,
					rcid:   rcid,
				}

				// Send the job to the bulk indexer
				idx.profileQueue <- &job
			default:
			}

		}
		return nil
	})
}
