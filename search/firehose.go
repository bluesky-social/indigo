package search

import (
	"bytes"
	"context"
	"fmt"
	"net/http"
	"strings"

	comatproto "github.com/bluesky-social/indigo/api/atproto"
	bsky "github.com/bluesky-social/indigo/api/bsky"
	"github.com/bluesky-social/indigo/backfill"
	"github.com/bluesky-social/indigo/events"
	"github.com/bluesky-social/indigo/events/schedulers/autoscaling"
	lexutil "github.com/bluesky-social/indigo/lex/util"
	"github.com/bluesky-social/indigo/repo"
	"github.com/bluesky-social/indigo/repomgr"
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
	s.bf.Start()

	d := websocket.DefaultDialer
	con, _, err := d.Dial(fmt.Sprintf("%s/xrpc/com.atproto.sync.subscribeRepos?cursor=%d", s.bgshost, cur), http.Header{})
	if err != nil {
		return fmt.Errorf("events dial failed: %w", err)
	}

	rsc := &events.RepoStreamCallbacks{
		RepoCommit: func(evt *comatproto.SyncSubscribeRepos_Commit) error {
			defer func() {
				if evt.Seq%50 == 0 {
					if err := s.updateLastCursor(evt.Seq); err != nil {
						log.Error("Failed to update cursor: ", err)
					}
				}
			}()
			if evt.TooBig && evt.Prev != nil {
				log.Errorf("skipping non-genesis too big events for now: %d", evt.Seq)
				return nil
			}

			if evt.TooBig {
				if err := s.processTooBigCommit(ctx, evt); err != nil {
					log.Errorf("failed to process tooBig event: %s", err)
					return nil
				}

				return nil
			}

			r, err := repo.ReadRepoFromCar(ctx, bytes.NewReader(evt.Blocks))
			if err != nil {
				log.Errorf("reading repo from car (seq: %d, len: %d): %w", evt.Seq, len(evt.Blocks), err)
				return nil
			}

			for _, op := range evt.Ops {
				ek := repomgr.EventKind(op.Action)
				switch ek {
				case repomgr.EvtKindCreateRecord, repomgr.EvtKindUpdateRecord:
					rc, rec, err := r.GetRecord(ctx, op.Path)
					if err != nil {
						e := fmt.Errorf("getting record %s (%s) within seq %d for %s: %w", op.Path, *op.Cid, evt.Seq, evt.Repo, err)
						log.Error(e)
						return nil
					}

					if lexutil.LexLink(rc) != *op.Cid {
						log.Errorf("mismatch in record and op cid: %s != %s", rc, *op.Cid)
						return nil
					}

					if err := s.handleOp(ctx, ek, evt.Seq, op.Path, evt.Repo, &rc, rec); err != nil {
						log.Errorf("failed to handle op: %s", err)
						return nil
					}

				case repomgr.EvtKindDeleteRecord:
					if err := s.handleOp(ctx, ek, evt.Seq, op.Path, evt.Repo, nil, nil); err != nil {
						log.Errorf("failed to handle delete: %s", err)
						return nil
					}
				}
			}

			return nil

		},
		RepoHandle: func(evt *comatproto.SyncSubscribeRepos_Handle) error {
			if err := s.updateUserHandle(ctx, evt.Did, evt.Handle); err != nil {
				log.Errorf("failed to update user handle: %s", err)
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

func (s *Server) handleCreateOrUpdate(ctx context.Context, did string, path string, recP *typegen.CBORMarshaler, rcid *cid.Cid) error {
	// Since this gets called in a backfill job, we need to check if the path is a post or profile
	if !strings.Contains(path, "app.bsky.feed.post") && !strings.Contains(path, "app.bsky.actor.profile") {
		return nil
	}

	u, err := s.getOrCreateUser(ctx, did)
	if err != nil {
		return fmt.Errorf("checking user: %w", err)
	}
	rec := *recP

	switch rec := rec.(type) {
	case *bsky.FeedPost:
		if err := s.indexPost(ctx, u, rec, path, *rcid); err != nil {
			return fmt.Errorf("indexing post: %w", err)
		}
	case *bsky.ActorProfile:
		if err := s.indexProfile(ctx, u, rec); err != nil {
			return fmt.Errorf("indexing profile: %w", err)
		}
	default:
	}
	return nil
}

func (s *Server) handleDelete(ctx context.Context, did string, path string) error {
	// Since this gets called in a backfill job, we need to check if the path is a post or profile
	if !strings.Contains(path, "app.bsky.feed.post") && !strings.Contains(path, "app.bsky.actor.profile") {
		return nil
	}

	u, err := s.getOrCreateUser(ctx, did)
	if err != nil {
		return err
	}

	switch {
	// TODO: handle profile deletes, its an edge case, but worth doing still
	case strings.Contains(path, "app.bsky.feed.post"):
		if err := s.deletePost(ctx, u, path); err != nil {
			return err
		}
	}

	return nil
}

func (s *Server) handleOp(ctx context.Context, op repomgr.EventKind, seq int64, path string, did string, rcid *cid.Cid, rec typegen.CBORMarshaler) error {
	var err error
	if !strings.Contains(path, "app.bsky.feed.post") && !strings.Contains(path, "app.bsky.actor.profile") {
		return nil
	}

	if op == repomgr.EvtKindCreateRecord || op == repomgr.EvtKindUpdateRecord {
		log.Infof("handling create(%d): %s - %s", seq, did, path)

		// Try to buffer the op, if it fails, we need to create a backfill job
		_, err := s.bfs.BufferOp(ctx, did, string(op), path, &rec, rcid)
		if err == backfill.ErrJobNotFound {
			log.Infof("no job found for repo %s, creating one", did)

			if err := s.bfs.EnqueueJob(did); err != nil {
				return fmt.Errorf("enqueueing job: %w", err)
			}

			// Try to buffer the op again so it gets picked up by the backfill job
			_, err = s.bfs.BufferOp(ctx, did, string(op), path, &rec, rcid)
			if err != nil {
				return fmt.Errorf("buffering op: %w", err)
			}
		} else if err == backfill.ErrJobComplete {
			// Backfill is done for this repo so we can just index it now
			err = s.handleCreateOrUpdate(ctx, did, path, &rec, rcid)
		}
	} else if op == repomgr.EvtKindDeleteRecord {
		log.Infof("handling delete(%d): %s - %s", seq, did, path)

		// Try to buffer the op, if it fails, we need to create a backfill job
		_, err := s.bfs.BufferOp(ctx, did, string(op), path, &rec, rcid)
		if err == backfill.ErrJobNotFound {
			log.Infof("no job found for repo %s, creating one", did)

			if err := s.bfs.EnqueueJob(did); err != nil {
				return fmt.Errorf("enqueueing job: %w", err)
			}

			// Try to buffer the op again so it gets picked up by the backfill job
			_, err = s.bfs.BufferOp(ctx, did, string(op), path, &rec, rcid)
			if err != nil {
				return fmt.Errorf("buffering op: %w", err)
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
	repodata, err := comatproto.SyncGetRepo(ctx, s.bgsxrpc, evt.Repo, "", evt.Commit.String())
	if err != nil {
		return err
	}

	r, err := repo.ReadRepoFromCar(ctx, bytes.NewReader(repodata))
	if err != nil {
		return err
	}

	u, err := s.getOrCreateUser(ctx, evt.Repo)
	if err != nil {
		return err
	}

	return r.ForEach(ctx, "", func(k string, v cid.Cid) error {
		if strings.HasPrefix(k, "app.bsky.feed.post") || strings.HasPrefix(k, "app.bsky.actor.profile") {
			rcid, rec, err := r.GetRecord(ctx, k)
			if err != nil {
				log.Errorf("failed to get record from repo checkout: %s", err)
				return nil
			}

			switch rec := rec.(type) {
			case *bsky.FeedPost:
				if err := s.indexPost(ctx, u, rec, k, rcid); err != nil {
					return fmt.Errorf("indexing post: %w", err)
				}
			case *bsky.ActorProfile:
				if err := s.indexProfile(ctx, u, rec); err != nil {
					return fmt.Errorf("indexing profile: %w", err)
				}
			default:
			}

		}
		return nil
	})
}
