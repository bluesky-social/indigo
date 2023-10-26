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
	"github.com/bluesky-social/indigo/repomgr"
	"github.com/bluesky-social/indigo/util/version"

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

func (s *Server) RunIndexer(ctx context.Context) error {
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
		"User-Agent": []string{fmt.Sprintf("palomar/%s", version.Version)},
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

			// Check if we've backfilled this repo, if not, we should enqueue it
			job, err := s.bfs.GetJob(ctx, evt.Repo)
			if job == nil && err == nil {
				logEvt.Info("enqueueing backfill job for new repo")
				if err := s.bfs.EnqueueJob(evt.Repo); err != nil {
					logEvt.Warn("failed to enqueue backfill job", "err", err)
				}
			}

			r, err := repo.ReadRepoFromCar(ctx, bytes.NewReader(evt.Blocks))
			if err != nil {
				// TODO: handle this case (instead of return nil)
				logEvt.Error("reading repo from car", "size_bytes", len(evt.Blocks), "err", err)
				return nil
			}

			for _, op := range evt.Ops {
				ek := repomgr.EventKind(op.Action)
				logOp := logEvt.With("op_path", op.Path, "op_cid", op.Cid)
				switch ek {
				case repomgr.EvtKindCreateRecord, repomgr.EvtKindUpdateRecord:
					rc, rec, err := r.GetRecord(ctx, op.Path)
					if err != nil {
						// TODO: handle this case (instead of return nil)
						logOp.Error("fetching record from event CAR slice", "err", err)
						return nil
					}

					if lexutil.LexLink(rc) != *op.Cid {
						// TODO: handle this case (instead of return nil)
						logOp.Error("mismatch in record and op cid", "record_cid", rc)
						return nil
					}

					if strings.HasPrefix(op.Path, "app.bsky.feed.post") {
						postsReceived.Inc()
					} else if strings.HasPrefix(op.Path, "app.bsky.actor.profile") {
						profilesReceived.Inc()
					}

					if err := s.handleOp(ctx, ek, evt.Seq, op.Path, evt.Repo, &rc, rec); err != nil {
						// TODO: handle this case (instead of return nil)
						logOp.Error("failed to handle event op", "err", err)
						return nil
					}

				case repomgr.EvtKindDeleteRecord:
					if err := s.handleOp(ctx, ek, evt.Seq, op.Path, evt.Repo, nil, nil); err != nil {
						// TODO: handle this case (instead of return nil)
						logOp.Error("failed to handle delete", "err", err)
						return nil
					}
				}
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
			if err := s.updateUserHandle(ctx, did, evt.Handle); err != nil {
				// TODO: handle this case (instead of return nil)
				s.logger.Error("failed to update user handle", "did", evt.Did, "handle", evt.Handle, "seq", evt.Seq, "err", err)
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

	switch rec := rec.(type) {
	case *bsky.FeedPost:
		if err := s.indexPost(ctx, ident, rec, path, *rcid); err != nil {
			postsFailed.Inc()
			return fmt.Errorf("indexing post for %s: %w", did.String(), err)
		}
		postsIndexed.Inc()
	case *bsky.ActorProfile:
		if err := s.indexProfile(ctx, ident, rec, path, *rcid); err != nil {
			profilesFailed.Inc()
			return fmt.Errorf("indexing profile for %s: %w", did.String(), err)
		}
		profilesIndexed.Inc()
	default:
	}
	return nil
}

func (s *Server) handleDelete(ctx context.Context, rawDID, path string) error {
	// Since this gets called in a backfill job, we need to check if the path is a post or profile
	if !strings.Contains(path, "app.bsky.feed.post") && !strings.Contains(path, "app.bsky.actor.profile") {
		return nil
	}

	did, err := syntax.ParseDID(rawDID)
	if err != nil {
		return fmt.Errorf("invalid DID in event: %w", err)
	}

	ident, err := s.dir.LookupDID(ctx, did)
	if err != nil {
		return err
	}
	if ident == nil {
		return fmt.Errorf("identity not found for did: %s", did.String())
	}

	switch {
	// TODO: handle profile deletes, its an edge case, but worth doing still
	case strings.Contains(path, "app.bsky.feed.post"):
		if err := s.deletePost(ctx, ident, path); err != nil {
			return err
		}
		postsDeleted.Inc()
	case strings.Contains(path, "app.bsky.actor.profile"):
		// profilesDeleted.Inc()
	}

	return nil
}

func (s *Server) handleOp(ctx context.Context, op repomgr.EventKind, seq int64, path string, did string, rcid *cid.Cid, rec typegen.CBORMarshaler) error {
	var err error
	if !strings.Contains(path, "app.bsky.feed.post") && !strings.Contains(path, "app.bsky.actor.profile") {
		return nil
	}

	if op == repomgr.EvtKindCreateRecord || op == repomgr.EvtKindUpdateRecord {
		s.logger.Debug("processing create record op", "seq", seq, "did", did, "path", path)

		// Try to buffer the op, if it fails, we need to create a backfill job
		_, err := s.bfs.BufferOp(ctx, did, string(op), path, &rec, rcid)
		if err == backfill.ErrJobNotFound {
			s.logger.Debug("no backfill job found for repo, creating one", "did", did)

			if err := s.bfs.EnqueueJob(did); err != nil {
				return fmt.Errorf("enqueueing backfill job: %w", err)
			}

			// Try to buffer the op again so it gets picked up by the backfill job
			_, err = s.bfs.BufferOp(ctx, did, string(op), path, &rec, rcid)
			if err != nil {
				return fmt.Errorf("buffering backfill op: %w", err)
			}
		} else if err == backfill.ErrJobComplete {
			// Backfill is done for this repo so we can just index it now
			err = s.handleCreateOrUpdate(ctx, did, path, &rec, rcid)
		}
	} else if op == repomgr.EvtKindDeleteRecord {
		s.logger.Debug("processing delete record op", "seq", seq, "did", did, "path", path)

		// Try to buffer the op, if it fails, we need to create a backfill job
		_, err := s.bfs.BufferOp(ctx, did, string(op), path, &rec, rcid)
		if err == backfill.ErrJobNotFound {
			s.logger.Debug("no backfill job found for repo, creating one", "did", did)

			if err := s.bfs.EnqueueJob(did); err != nil {
				return fmt.Errorf("enqueueing backfill job: %w", err)
			}

			// Try to buffer the op again so it gets picked up by the backfill job
			_, err = s.bfs.BufferOp(ctx, did, string(op), path, &rec, rcid)
			if err != nil {
				return fmt.Errorf("buffering backfill op: %w", err)
			}
		} else if err == backfill.ErrJobComplete {
			// Backfill is done for this repo so we can delete imemdiately
			err = s.handleDelete(ctx, did, path)
		}
	}

	if err != nil {
		return fmt.Errorf("failed to handle op: %w", err)
	}

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

	ident, err := s.dir.LookupDID(ctx, did)
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
				s.logger.Error("failed to get record from repo checkout", "path", k, "err", err)
				return nil
			}

			switch rec := rec.(type) {
			case *bsky.FeedPost:
				if err := s.indexPost(ctx, ident, rec, k, rcid); err != nil {
					return fmt.Errorf("indexing post: %w", err)
				}
			case *bsky.ActorProfile:
				if err := s.indexProfile(ctx, ident, rec, k, rcid); err != nil {
					return fmt.Errorf("indexing profile: %w", err)
				}
			default:
			}

		}
		return nil
	})
}
