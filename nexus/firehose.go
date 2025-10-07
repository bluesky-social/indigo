package main

import (
	"context"
	"fmt"
	"log/slog"
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

func (nexus *Nexus) SubscribeFirehose(ctx context.Context) error {
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
			return nexus.handleCommitEvent(ctx, evt)
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
	slog.Info("starting firehose consumer", "relayHost", relayHost)
	err = events.HandleRepoStream(ctx, con, scheduler, nil)

	if err != nil {
		return err
	}
	return events.HandleRepoStream(ctx, con, scheduler, nil)
}

func (nexus *Nexus) handleCommitEvent(ctx context.Context, evt *comatproto.SyncSubscribeRepos_Commit) error {
	nexus.mu.RLock()
	exists := nexus.filterDids[evt.Repo]
	nexus.mu.RUnlock()

	if !exists {
		return nil
	}

	// Check repo state from database
	state, err := nexus.GetRepoState(evt.Repo)
	if err != nil {
		nexus.logger.Error("failed to get repo state", "did", evt.Repo, "error", err)
		return nil // Don't fail on state lookup errors
	}

	r, err := repo.VerifyCommitMessage(ctx, evt)
	if err != nil {
		nexus.logger.Info("failed to verify commit", "did", evt.Repo, "err", err)
		return err
	}

	for _, op := range evt.Ops {
		coll, rkey, err := syntax.ParseRepoPath(op.Path)
		if err != nil {
			return err
		}

		outOp := &Op{
			Did:        evt.Repo,
			Collection: coll.String(),
			Rkey:       rkey.String(),
			Action:     op.Action,
		}

		// For creates and updates, get the record
		if op.Action == "create" || op.Action == "update" {
			recBytes, recCid, err := r.GetRecordBytes(ctx, coll, rkey)
			if err != nil {
				return err
			}
			rec, err := data.UnmarshalCBOR(recBytes)
			if err != nil {
				return err
			}
			outOp.Record = rec
			outOp.Cid = recCid.String()
		}

		// If backfill is in progress, buffer to memory
		if state == models.RepoStatePending || state == models.RepoStateBackfilling {
			nexus.logger.Debug("buffering event to memory during backfill", "did", evt.Repo, "state", state)
			nexus.mu.Lock()
			nexus.backfillBuffer[evt.Repo] = append(nexus.backfillBuffer[evt.Repo], outOp)
			nexus.mu.Unlock()
		} else {
			// Normal flow - send through outbox
			err = nexus.outbox.Send(outOp)
			if err != nil {
				return err
			}
		}
	}

	// Update rev if repo is active
	if state == models.RepoStateActive {
		if err := nexus.UpdateRepoState(evt.Repo, models.RepoStateActive, evt.Rev, ""); err != nil {
			nexus.logger.Error("failed to update rev", "did", evt.Repo, "error", err)
		}
	}

	return nil
}
