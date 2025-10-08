package main

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"sync/atomic"

	comatproto "github.com/bluesky-social/indigo/api/atproto"
	"github.com/bluesky-social/indigo/atproto/data"
	"github.com/bluesky-social/indigo/atproto/repo"
	"github.com/bluesky-social/indigo/atproto/syntax"
	"github.com/bluesky-social/indigo/events"
	"github.com/bluesky-social/indigo/events/schedulers/parallel"
	"github.com/bluesky-social/indigo/nexus/models"
	"github.com/gorilla/websocket"
)

const persistCursorEvery = 100

func (n *Nexus) SubscribeFirehose(ctx context.Context) error {
	cur, err := n.ReadLastCursor(ctx)
	if err != nil {
		return err
	}

	dialer := websocket.DefaultDialer
	u, err := url.Parse(n.RelayHost)
	if err != nil {
		return fmt.Errorf("invalid relayHost URI: %w", err)
	}
	switch u.Scheme {
	case "http":
		u.Scheme = "ws"
	case "https":
		u.Scheme = "wss"
	}
	u.Path = "xrpc/com.atproto.sync.subscribeRepos"
	if cur != 0 {
		u.RawQuery = fmt.Sprintf("cursor=%d", cur)
	}
	urlString := u.String()
	n.logger.Info("subscribing to firehose", "relayHost", n.RelayHost, "cursor", cur)
	con, _, err := dialer.Dial(urlString, http.Header{})
	if err != nil {
		return fmt.Errorf("subscribing to firehose failed (dialing): %w", err)
	}

	var lastSeq atomic.Uint64
	var eventCount atomic.Uint64

	rsc := &events.RepoStreamCallbacks{
		RepoCommit: func(evt *comatproto.SyncSubscribeRepos_Commit) error {
			lastSeq.Swap(uint64(evt.Seq))
			if eventCount.Add(1)%persistCursorEvery == 0 {
				if err := n.PersistCursor(ctx, evt.Seq); err != nil {
					n.logger.Error("failed to persist cursor", "seq", evt.Seq, "error", err)
				}
			}
			return n.handleCommitEvent(ctx, evt)
		},
		RepoSync: func(evt *comatproto.SyncSubscribeRepos_Sync) error {
			return nil
		},
		RepoIdentity: func(evt *comatproto.SyncSubscribeRepos_Identity) error {
			lastSeq.Swap(uint64(evt.Seq))
			if eventCount.Add(1)%persistCursorEvery == 0 {
				if err := n.PersistCursor(ctx, evt.Seq); err != nil {
					n.logger.Error("failed to persist cursor", "seq", evt.Seq, "error", err)
				}
			}
			return nil
		},
		RepoAccount: func(evt *comatproto.SyncSubscribeRepos_Account) error {
			lastSeq.Swap(uint64(evt.Seq))
			if eventCount.Add(1)%persistCursorEvery == 0 {
				if err := n.PersistCursor(ctx, evt.Seq); err != nil {
					n.logger.Error("failed to persist cursor", "seq", evt.Seq, "error", err)
				}
			}
			return nil
		},
	}

	scheduler := parallel.NewScheduler(
		10,
		100,
		n.RelayHost,
		rsc.EventHandler,
	)
	return events.HandleRepoStream(ctx, con, scheduler, nil)
}

func (n *Nexus) handleCommitEvent(ctx context.Context, evt *comatproto.SyncSubscribeRepos_Commit) error {
	if !n.filter.Contains(evt.Repo) {
		return nil
	}

	state, err := n.GetRepoState(evt.Repo)
	if err != nil {
		n.logger.Error("failed to get repo state", "did", evt.Repo, "error", err)
		return nil
	}

	if state == models.RepoStatePending {
		return nil
	} else if state == models.RepoStateBackfilling {
		return n.bufferCommitEvent(evt)
	}

	r, err := repo.VerifyCommitMessage(ctx, evt)
	if err != nil {
		n.logger.Info("failed to verify commit", "did", evt.Repo, "error", err)
		return err
	}

	for _, op := range evt.Ops {
		coll, rkey, err := syntax.ParseRepoPath(op.Path)
		if err != nil {
			return err
		}

		collStr := coll.String()
		rkeyStr := rkey.String()

		if op.Action == "delete" {
			outOp := &Op{
				Did:        evt.Repo,
				Collection: collStr,
				Rkey:       rkeyStr,
				Action:     "delete",
			}

			if err := n.outbox.Send(outOp); err != nil {
				return err
			}

			if err := n.db.Where("did = ? AND collection = ? AND rkey = ?", evt.Repo, collStr, rkeyStr).Delete(&models.RepoRecord{}).Error; err != nil {
				n.logger.Error("failed to delete repo record", "did", evt.Repo, "path", op.Path, "error", err)
			}
			continue
		}

		recBytes, recCid, err := r.GetRecordBytes(ctx, coll, rkey)
		if err != nil {
			return err
		}
		cidStr := recCid.String()

		rec, err := data.UnmarshalCBOR(recBytes)
		if err != nil {
			return err
		}

		outOp := &Op{
			Did:        evt.Repo,
			Collection: collStr,
			Rkey:       rkeyStr,
			Action:     op.Action,
			Record:     rec,
			Cid:        cidStr,
		}

		if err := n.outbox.Send(outOp); err != nil {
			return err
		}

		repoRecord := models.RepoRecord{
			Did:        evt.Repo,
			Collection: collStr,
			Rkey:       rkeyStr,
			Cid:        cidStr,
			Rev:        evt.Rev,
		}
		if err := n.db.Save(&repoRecord).Error; err != nil {
			n.logger.Error("failed to save repo record", "did", evt.Repo, "path", op.Path, "error", err)
		}
	}

	if err := n.UpdateRepoState(evt.Repo, models.RepoStateActive, evt.Rev, ""); err != nil {
		n.logger.Error("failed to update rev", "did", evt.Repo, "error", err)
	}

	return nil
}

func (n *Nexus) bufferCommitEvent(evt *comatproto.SyncSubscribeRepos_Commit) error {
	for _, op := range evt.Ops {
		bufferedEvt := models.BufferedEvt{
			Did:        evt.Repo,
			Collection: op.Path[:len(op.Path)-len(op.Path[len(op.Path)-1:])], // extract collection from path
			Rkey:       op.Path[len(op.Path)-1:],                             // extract rkey from path
			Action:     op.Action,
			Cid:        op.Cid.String(),
		}
		if err := n.db.Create(&bufferedEvt).Error; err != nil {
			n.logger.Error("failed to buffer event", "did", evt.Repo, "path", op.Path, "error", err)
		}
	}
	return nil
}
