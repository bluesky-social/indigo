package main

import (
	"bytes"
	"context"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	comatproto "github.com/bluesky-social/indigo/api/atproto"
	//bsky "github.com/bluesky-social/indigo/api/bsky"
	"github.com/bluesky-social/indigo/atproto/syntax"
	"github.com/bluesky-social/indigo/events"
	"github.com/bluesky-social/indigo/events/schedulers/autoscaling"
	"github.com/bluesky-social/indigo/repo"

	"github.com/carlmjohnson/versioninfo"
	"github.com/gorilla/websocket"
	"github.com/ipfs/go-cid"
	typegen "github.com/whyrusleeping/cbor-gen"
)

func (s *Server) getLastCursor() (int64, error) {
	var lastSeq LastSeq
	if err := s.db.Find(&lastSeq).Error; err != nil {
		return 0, err
	}

	if lastSeq.ID == 0 {
		return 0, s.db.Create(&lastSeq).Error
	}

	return lastSeq.Seq, nil
}

func (s *Server) updateLastCursor(curs int64) error {
	return s.db.Model(LastSeq{}).Where("id = 1").Update("seq", curs).Error
}

func (s *Server) Run(ctx context.Context) error {
	cur, err := s.getLastCursor()
	if err != nil {
		return fmt.Errorf("get last cursor: %w", err)
	}

	err = s.bfs.LoadJobs(ctx)
	if err != nil {
		return fmt.Errorf("loading backfill jobs: %w", err)
	}
	go s.bf.Start()
	go s.discoverRepos()

	d := websocket.DefaultDialer
	u, err := url.Parse(s.bgshost)
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
					if err := s.updateLastCursor(evt.Seq); err != nil {
						s.logger.Error("failed to persist cursor", "err", err)
					}
				}
			}()
			logEvt := s.logger.With("repo", evt.Repo, "rev", evt.Rev, "seq", evt.Seq)
			if evt.TooBig && evt.Prev != nil {
				// TODO: handle this case (instead of return nil)
				logEvt.Error("skipping non-genesis tooBig events for now")
				return nil
			}

			if evt.TooBig {
				if err := s.processTooBigCommit(ctx, evt); err != nil {
					// TODO: handle this case (instead of return nil)
					logEvt.Error("failed to process tooBig event", "err", err)
					return nil
				}

				return nil
			}

			if !s.skipBackfill {
				// Check if we've backfilled this repo, if not, we should enqueue it
				job, err := s.bfs.GetJob(ctx, evt.Repo)
				if job == nil && err == nil {
					logEvt.Info("enqueueing backfill job for new repo")
					if err := s.bfs.EnqueueJob(evt.Repo); err != nil {
						logEvt.Warn("failed to enqueue backfill job", "err", err)
					}
				}
			}

			if err = s.engine.ProcessCommit(ctx, evt); err != nil {
				// TODO: handle this, instead of return nul
				logEvt.Error("failed to process commit", "err", err)
				return nil
			}

			return nil

		},
		RepoHandle: func(evt *comatproto.SyncSubscribeRepos_Handle) error {
			ctx := context.Background()
			ctx, span := tracer.Start(ctx, "RepoHandle")
			defer span.End()

			did, err := syntax.ParseDID(evt.Did)
			if err != nil {
				s.logger.Error("bad DID in RepoHandle event", "did", evt.Did, "handle", evt.Handle, "seq", evt.Seq, "err", err)
				return nil
			}
			if err := s.engine.ProcessIdentityEvent("handle", did); err != nil {
				s.logger.Error("processing handle update failed", "did", evt.Did, "handle", evt.Handle, "seq", evt.Seq, "err", err)
			}
			return nil
		},
	}

	return events.HandleRepoStream(
		ctx, con, autoscaling.NewScheduler(
			autoscaling.DefaultAutoscaleSettings(),
			s.bgshost,
			rsc.EventHandler,
		),
	)
}

func (s *Server) discoverRepos() {
	if s.skipBackfill {
		s.logger.Info("skipping repo discovery")
		return
	}

	ctx := context.Background()
	log := s.logger.With("func", "discoverRepos")
	log.Info("starting repo discovery")

	cursor := ""
	limit := int64(500)

	totalEnqueued := 0
	totalSkipped := 0
	totalErrored := 0

	for {
		resp, err := comatproto.SyncListRepos(ctx, s.bgsxrpc, cursor, limit)
		if err != nil {
			log.Error("failed to list repos", "err", err)
			time.Sleep(5 * time.Second)
			continue
		}
		log.Info("got repo page", "count", len(resp.Repos), "cursor", resp.Cursor)
		enqueued := 0
		skipped := 0
		errored := 0
		for _, repo := range resp.Repos {
			job, err := s.bfs.GetJob(ctx, repo.Did)
			if job == nil && err == nil {
				log.Info("enqueuing backfill job for new repo", "did", repo.Did)
				if err := s.bfs.EnqueueJob(repo.Did); err != nil {
					log.Warn("failed to enqueue backfill job", "err", err)
					errored++
					continue
				}
				enqueued++
			} else if err != nil {
				log.Warn("failed to get backfill job", "did", repo.Did, "err", err)
				errored++
			} else {
				skipped++
			}
		}
		log.Info("enqueued repos", "enqueued", enqueued, "skipped", skipped, "errored", errored)
		totalEnqueued += enqueued
		totalSkipped += skipped
		totalErrored += errored
		if resp.Cursor != nil && *resp.Cursor != "" {
			cursor = *resp.Cursor
		} else {
			break
		}
	}

	log.Info("finished repo discovery", "totalEnqueued", totalEnqueued, "totalSkipped", totalSkipped, "totalErrored", totalErrored)
}

func (s *Server) handleCreateOrUpdate(ctx context.Context, rawDID string, path string, recP *typegen.CBORMarshaler, rcid *cid.Cid) error {
	// Since this gets called in a backfill job, we need to check if the path is a post or profile
	if !strings.Contains(path, "app.bsky.feed.post") && !strings.Contains(path, "app.bsky.actor.profile") {
		return nil
	}

	did, err := syntax.ParseDID(rawDID)
	if err != nil {
		return fmt.Errorf("bad DID syntax in event: %w", err)
	}

	ident, err := s.dir.LookupDID(ctx, did)
	if err != nil {
		return fmt.Errorf("resolving identity: %w", err)
	}
	if ident == nil {
		return fmt.Errorf("identity not found for did: %s", did.String())
	}
	rec := *recP

	_ = rec
	/* XXX:
	switch rec := rec.(type) {
	case *bsky.FeedPost:
		// XXX: if err := s.indexPost(ctx, ident, rec, path, *rcid); err != nil {
		_ = rec
		if err := s.engine.ProcessCommit(ctx, evt); err != nil {
			postsFailed.Inc()
			return fmt.Errorf("processing post for %s: %w", did.String(), err)
		}
		postsIndexed.Inc()
	case *bsky.ActorProfile:
		// XXX: if err := s.indexProfile(ctx, ident, rec, path, *rcid); err != nil {
		if err := s.engine.ProcessCommit(ctx, evt); err != nil {
			profilesFailed.Inc()
			return fmt.Errorf("processing profile for %s: %w", did.String(), err)
		}
		profilesIndexed.Inc()
	default:
	}
	*/
	return nil
}

func (s *Server) handleDelete(ctx context.Context, rawDID, path string) error {
	// TODO: just ignoring for now
	return nil
}

func (s *Server) processTooBigCommit(ctx context.Context, evt *comatproto.SyncSubscribeRepos_Commit) error {
	repodata, err := comatproto.SyncGetRepo(ctx, s.bgsxrpc, evt.Repo, "")
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

	return r.ForEach(ctx, "", func(k string, v cid.Cid) error {
		if strings.HasPrefix(k, "app.bsky.feed.post") || strings.HasPrefix(k, "app.bsky.actor.profile") {
			rcid, rec, err := r.GetRecord(ctx, k)
			if err != nil {
				// TODO: handle this case (instead of return nil)
				s.logger.Error("failed to get record from repo checkout", "path", k, "err", err)
				return nil
			}

			// TODO: may want to treat this as a regular event?
			_ = rcid
			_ = did
			_ = rec
			/* XXX:
			if err := s.engine.ProcessRecord(ctx, did, m, rec); err != nil {
				return fmt.Errorf("processing record from tooBig commit: %w", err)
			}
			*/
		}
		return nil
	})
}
