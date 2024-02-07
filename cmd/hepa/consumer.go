package main

import (
	"bytes"
	"context"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"sync/atomic"

	comatproto "github.com/bluesky-social/indigo/api/atproto"
	"github.com/bluesky-social/indigo/atproto/syntax"
	"github.com/bluesky-social/indigo/automod"
	"github.com/bluesky-social/indigo/events/schedulers/parallel"
	lexutil "github.com/bluesky-social/indigo/lex/util"

	"github.com/bluesky-social/indigo/events"
	"github.com/bluesky-social/indigo/repo"
	"github.com/bluesky-social/indigo/repomgr"
	"github.com/carlmjohnson/versioninfo"
	"github.com/gorilla/websocket"
)

func (s *Server) RunConsumer(ctx context.Context) error {

	// TODO: persist cursor in a database or local disk
	cur, err := s.ReadLastCursor(ctx)
	if err != nil {
		return err
	}

	dialer := websocket.DefaultDialer
	u, err := url.Parse(s.bgshost)
	if err != nil {
		return fmt.Errorf("invalid bgshost URI: %w", err)
	}
	u.Path = "xrpc/com.atproto.sync.subscribeRepos"
	if cur != 0 {
		u.RawQuery = fmt.Sprintf("cursor=%d", cur)
	}
	s.logger.Info("subscribing to repo event stream", "upstream", s.bgshost, "cursor", cur)
	con, _, err := dialer.Dial(u.String(), http.Header{
		"User-Agent": []string{fmt.Sprintf("hepa/%s", versioninfo.Short())},
	})
	if err != nil {
		return fmt.Errorf("subscribing to firehose failed (dialing): %w", err)
	}

	rsc := &events.RepoStreamCallbacks{
		RepoCommit: func(evt *comatproto.SyncSubscribeRepos_Commit) error {
			atomic.StoreInt64(&s.lastSeq, evt.Seq)
			return s.HandleRepoCommit(ctx, evt)
		},
		RepoHandle: func(evt *comatproto.SyncSubscribeRepos_Handle) error {
			atomic.StoreInt64(&s.lastSeq, evt.Seq)
			did, err := syntax.ParseDID(evt.Did)
			if err != nil {
				s.logger.Error("bad DID in RepoHandle event", "did", evt.Did, "handle", evt.Handle, "seq", evt.Seq, "err", err)
				return nil
			}
			if err := s.engine.ProcessIdentityEvent(ctx, "handle", did); err != nil {
				s.logger.Error("processing handle update failed", "did", evt.Did, "handle", evt.Handle, "seq", evt.Seq, "err", err)
			}
			return nil
		},
		RepoMigrate: func(evt *comatproto.SyncSubscribeRepos_Migrate) error {
			atomic.StoreInt64(&s.lastSeq, evt.Seq)
			did, err := syntax.ParseDID(evt.Did)
			if err != nil {
				s.logger.Error("bad DID in RepoMigrate event", "did", evt.Did, "seq", evt.Seq, "err", err)
				return nil
			}
			if err := s.engine.ProcessIdentityEvent(ctx, "migrate", did); err != nil {
				s.logger.Error("processing repo migrate failed", "did", evt.Did, "seq", evt.Seq, "err", err)
			}
			return nil
		},
		// TODO: other event callbacks as needed
	}

	// start at higher parallelism (somewhat arbitrary)
	return events.HandleRepoStream(
		ctx, con, parallel.NewScheduler(
			200,
			1000,
			s.bgshost,
			rsc.EventHandler,
		),
	)
}

// TODO: move this to a "ParsePath" helper in syntax package?
func splitRepoPath(path string) (syntax.NSID, syntax.RecordKey, error) {
	parts := strings.SplitN(path, "/", 3)
	if len(parts) != 2 {
		return "", "", fmt.Errorf("invalid record path: %s", path)
	}
	collection, err := syntax.ParseNSID(parts[0])
	if err != nil {
		return "", "", err
	}
	rkey, err := syntax.ParseRecordKey(parts[1])
	if err != nil {
		return "", "", err
	}
	return collection, rkey, nil
}

// NOTE: for now, this function basically never errors, just logs and returns nil. Should think through error processing better.
func (s *Server) HandleRepoCommit(ctx context.Context, evt *comatproto.SyncSubscribeRepos_Commit) error {

	logger := s.logger.With("event", "commit", "did", evt.Repo, "rev", evt.Rev, "seq", evt.Seq)
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

	// empty commit is a special case, temporarily, basically indicates "new account"
	if len(evt.Ops) == 0 {
		if err := s.engine.ProcessIdentityEvent(ctx, "create", did); err != nil {
			s.logger.Error("processing handle update failed", "did", evt.Repo, "rev", evt.Rev, "seq", evt.Seq, "err", err)
		}
	}

	for _, op := range evt.Ops {
		logger = logger.With("eventKind", op.Action, "path", op.Path)
		collection, rkey, err := splitRepoPath(op.Path)
		if err != nil {
			logger.Error("invalid path in repo op")
			return nil
		}

		ek := repomgr.EventKind(op.Action)
		switch ek {
		case repomgr.EvtKindCreateRecord, repomgr.EvtKindUpdateRecord:
			// read the record from blocks, and verify CID
			rc, rec, err := rr.GetRecord(ctx, op.Path)
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
			err = s.engine.ProcessRecordOp(ctx, automod.RecordOp{
				Action:     action,
				DID:        did,
				Collection: collection,
				RecordKey:  rkey,
				CID:        &recCID,
				Value:      rec,
			})
			if err != nil {
				logger.Error("engine failed to process record", "err", err)
				continue
			}
		case repomgr.EvtKindDeleteRecord:
			err = s.engine.ProcessRecordOp(ctx, automod.RecordOp{
				Action:     automod.DeleteOp,
				DID:        did,
				Collection: collection,
				RecordKey:  rkey,
				CID:        nil,
				Value:      nil,
			})
			if err != nil {
				logger.Error("engine failed to process record", "err", err)
				continue
			}
		default:
			// TODO: other event types: update, delete
		}
	}

	return nil
}
