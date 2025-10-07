package main

import (
	"context"
	"fmt"
	"net/http"
	"net/url"

	comatproto "github.com/bluesky-social/indigo/api/atproto"
	"github.com/bluesky-social/indigo/atproto/data"
	"github.com/bluesky-social/indigo/atproto/repo"
	"github.com/bluesky-social/indigo/atproto/syntax"
	"github.com/bluesky-social/indigo/events"
	"github.com/bluesky-social/indigo/events/schedulers/parallel"
	"github.com/bluesky-social/indigo/nexus/models"
	"github.com/gorilla/websocket"
)

func (n *Nexus) SubscribeFirehose(ctx context.Context) error {
	relayHost := "https://bsky.network"

	dialer := websocket.DefaultDialer
	u, err := url.Parse(relayHost)
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
	urlString := u.String()
	con, _, err := dialer.Dial(urlString, http.Header{})
	if err != nil {
		return fmt.Errorf("subscribing to firehose failed (dialing): %w", err)
	}

	rsc := &events.RepoStreamCallbacks{
		RepoCommit: func(evt *comatproto.SyncSubscribeRepos_Commit) error {
			return n.handleCommitEvent(ctx, evt)
		},
		RepoSync: func(evt *comatproto.SyncSubscribeRepos_Sync) error {
			return nil
		},
		RepoIdentity: func(evt *comatproto.SyncSubscribeRepos_Identity) error {
			return nil
		},
		RepoAccount: func(evt *comatproto.SyncSubscribeRepos_Account) error {
			return nil
		},
	}

	scheduler := parallel.NewScheduler(
		1,
		100,
		relayHost,
		rsc.EventHandler,
	)
	n.logger.Info("starting firehose consumer", "relayHost", relayHost)
	return events.HandleRepoStream(ctx, con, scheduler, nil)
}

func (n *Nexus) handleCommitEvent(ctx context.Context, evt *comatproto.SyncSubscribeRepos_Commit) error {
	if !n.filter.Contains(evt.Repo) {
		return nil
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
