package relay

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"time"

	comatproto "github.com/bluesky-social/indigo/api/atproto"
	"github.com/bluesky-social/indigo/atproto/syntax"
	"github.com/bluesky-social/indigo/cmd/relayered/relay/slurper"
	"github.com/bluesky-social/indigo/cmd/relayered/stream"

	"github.com/ipfs/go-cid"
	"go.opentelemetry.io/otel/attribute"
	"gorm.io/gorm"
)

// handleFedEvent() is the callback passed to Slurper called from Slurper.handleConnection()
func (r *Relay) handleFedEvent(ctx context.Context, host *slurper.PDS, env *stream.XRPCStreamEvent) error {
	ctx, span := tracer.Start(ctx, "handleFedEvent")
	defer span.End()

	start := time.Now()
	defer func() {
		eventsHandleDuration.WithLabelValues(host.Host).Observe(time.Since(start).Seconds())
	}()

	EventsReceivedCounter.WithLabelValues(host.Host).Add(1)

	switch {
	case env.RepoCommit != nil:
		repoCommitsReceivedCounter.WithLabelValues(host.Host).Add(1)
		return r.handleCommit(ctx, host, env.RepoCommit)
	case env.RepoSync != nil:
		repoSyncReceivedCounter.WithLabelValues(host.Host).Add(1)
		return r.handleSync(ctx, host, env.RepoSync)
	case env.RepoHandle != nil:
		eventsWarningsCounter.WithLabelValues(host.Host, "handle").Add(1)
		// TODO: rate limit warnings per PDS before we (temporarily?) block them
		return nil
	case env.RepoIdentity != nil:
		r.Logger.Info("relay got identity event", "did", env.RepoIdentity.Did)
		// Flush any cached DID documents for this user
		r.purgeDidCache(ctx, env.RepoIdentity.Did)

		// Refetch the DID doc and update our cached keys and handle etc.
		account, err := r.syncPDSAccount(ctx, env.RepoIdentity.Did, host, nil)
		if err != nil {
			return err
		}

		// Broadcast the identity event to all consumers
		err = r.Events.AddEvent(ctx, &stream.XRPCStreamEvent{
			RepoIdentity: &comatproto.SyncSubscribeRepos_Identity{
				Did:    env.RepoIdentity.Did,
				Seq:    env.RepoIdentity.Seq,
				Time:   env.RepoIdentity.Time,
				Handle: env.RepoIdentity.Handle,
			},
			PrivUid: account.ID,
		})
		if err != nil {
			r.Logger.Error("failed to broadcast Identity event", "error", err, "did", env.RepoIdentity.Did)
			return fmt.Errorf("failed to broadcast Identity event: %w", err)
		}

		return nil
	case env.RepoAccount != nil:
		span.SetAttributes(
			attribute.String("did", env.RepoAccount.Did),
			attribute.Int64("seq", env.RepoAccount.Seq),
			attribute.Bool("active", env.RepoAccount.Active),
		)

		if env.RepoAccount.Status != nil {
			span.SetAttributes(attribute.String("repo_status", *env.RepoAccount.Status))
		}
		r.Logger.Info("relay got account event", "did", env.RepoAccount.Did)

		if !env.RepoAccount.Active && env.RepoAccount.Status == nil {
			// TODO: semantics here aren't really clear
			r.Logger.Warn("dropping invalid account event", "did", env.RepoAccount.Did, "active", env.RepoAccount.Active, "status", env.RepoAccount.Status)
			accountVerifyWarnings.WithLabelValues(host.Host, "nostat").Inc()
			return nil
		}

		// Flush any cached DID documents for this user
		r.purgeDidCache(ctx, env.RepoAccount.Did)

		// Refetch the DID doc to make sure the PDS is still authoritative
		account, err := r.syncPDSAccount(ctx, env.RepoAccount.Did, host, nil)
		if err != nil {
			span.RecordError(err)
			return err
		}

		// Check if the PDS is still authoritative
		// if not we don't want to be propagating this account event
		if account.GetPDS() != host.ID && !r.Config.SkipAccountHostCheck {
			r.Logger.Error("account event from non-authoritative pds",
				"seq", env.RepoAccount.Seq,
				"did", env.RepoAccount.Did,
				"event_from", host.Host,
				"did_doc_declared_pds", account.GetPDS(),
				"account_evt", env.RepoAccount,
			)
			return fmt.Errorf("event from non-authoritative pds")
		}

		// Process the account status change
		repoStatus := slurper.AccountStatusActive
		if !env.RepoAccount.Active && env.RepoAccount.Status != nil {
			repoStatus = *env.RepoAccount.Status
		}

		account.SetUpstreamStatus(repoStatus)
		err = r.db.Save(account).Error
		if err != nil {
			span.RecordError(err)
			return fmt.Errorf("failed to update account status: %w", err)
		}

		shouldBeActive := env.RepoAccount.Active
		status := env.RepoAccount.Status

		// override with local status
		if account.GetTakenDown() {
			shouldBeActive = false
			status = &slurper.AccountStatusTakendown
		}

		// Broadcast the account event to all consumers
		err = r.Events.AddEvent(ctx, &stream.XRPCStreamEvent{
			RepoAccount: &comatproto.SyncSubscribeRepos_Account{
				Active: shouldBeActive,
				Did:    env.RepoAccount.Did,
				Seq:    env.RepoAccount.Seq,
				Status: status,
				Time:   env.RepoAccount.Time,
			},
			PrivUid: account.ID,
		})
		if err != nil {
			r.Logger.Error("failed to broadcast Account event", "error", err, "did", env.RepoAccount.Did)
			return fmt.Errorf("failed to broadcast Account event: %w", err)
		}

		return nil
	case env.RepoMigrate != nil:
		eventsWarningsCounter.WithLabelValues(host.Host, "migrate").Add(1)
		// TODO: rate limit warnings per PDS before we (temporarily?) block them
		return nil
	case env.RepoTombstone != nil:
		eventsWarningsCounter.WithLabelValues(host.Host, "tombstone").Add(1)
		// TODO: rate limit warnings per PDS before we (temporarily?) block them
		return nil
	default:
		return fmt.Errorf("invalid fed event")
	}
}

func (r *Relay) handleCommit(ctx context.Context, host *slurper.PDS, evt *comatproto.SyncSubscribeRepos_Commit) error {
	r.Logger.Debug("relay got repo append event", "seq", evt.Seq, "pdsHost", host.Host, "repo", evt.Repo)

	account, err := r.LookupUserByDid(ctx, evt.Repo)
	if err != nil {
		if !errors.Is(err, gorm.ErrRecordNotFound) {
			repoCommitsResultCounter.WithLabelValues(host.Host, "nou").Inc()
			return fmt.Errorf("looking up event user: %w", err)
		}

		account, err = r.newUser(ctx, host, evt.Repo)
		if err != nil {
			repoCommitsResultCounter.WithLabelValues(host.Host, "nuerr").Inc()
			return err
		}
	}
	if account == nil {
		repoCommitsResultCounter.WithLabelValues(host.Host, "nou2").Inc()
		return ErrCommitNoUser
	}

	ustatus := account.GetUpstreamStatus()

	if account.GetTakenDown() || ustatus == slurper.AccountStatusTakendown {
		r.Logger.Debug("dropping commit event from taken down user", "did", evt.Repo, "seq", evt.Seq, "pdsHost", host.Host)
		repoCommitsResultCounter.WithLabelValues(host.Host, "tdu").Inc()
		return nil
	}

	if ustatus == slurper.AccountStatusSuspended {
		r.Logger.Debug("dropping commit event from suspended user", "did", evt.Repo, "seq", evt.Seq, "pdsHost", host.Host)
		repoCommitsResultCounter.WithLabelValues(host.Host, "susu").Inc()
		return nil
	}

	if ustatus == slurper.AccountStatusDeactivated {
		r.Logger.Debug("dropping commit event from deactivated user", "did", evt.Repo, "seq", evt.Seq, "pdsHost", host.Host)
		repoCommitsResultCounter.WithLabelValues(host.Host, "du").Inc()
		return nil
	}

	if evt.Rebase {
		repoCommitsResultCounter.WithLabelValues(host.Host, "rebase").Inc()
		return fmt.Errorf("rebase was true in event seq:%d,host:%s", evt.Seq, host.Host)
	}

	accountPDSId := account.GetPDS()
	if host.ID != accountPDSId && accountPDSId != 0 {
		r.Logger.Warn("received event for repo from different pds than expected", "repo", evt.Repo, "expPds", accountPDSId, "gotPds", host.Host)
		// Flush any cached DID documents for this user
		r.purgeDidCache(ctx, evt.Repo)

		account, err = r.syncPDSAccount(ctx, evt.Repo, host, account)
		if err != nil {
			repoCommitsResultCounter.WithLabelValues(host.Host, "uerr2").Inc()
			return err
		}

		if account.GetPDS() != host.ID && !r.Config.SkipAccountHostCheck {
			repoCommitsResultCounter.WithLabelValues(host.Host, "noauth").Inc()
			return fmt.Errorf("event from non-authoritative pds")
		}
	}

	var prevState slurper.AccountPreviousState
	err = r.db.First(&prevState, account.ID).Error
	prevP := &prevState
	if errors.Is(err, gorm.ErrRecordNotFound) {
		prevP = nil
	} else if err != nil {
		r.Logger.Error("failed to get previous root", "err", err)
		prevP = nil
	}
	dbPrevRootStr := ""
	dbPrevSeqStr := ""
	if prevP != nil {
		if prevState.Seq >= evt.Seq && ((prevState.Seq - evt.Seq) < 2000) {
			// ignore catchup overlap of 200 on some subscribeRepos restarts
			repoCommitsResultCounter.WithLabelValues(host.Host, "dup").Inc()
			return nil
		}
		dbPrevRootStr = prevState.Cid.CID.String()
		dbPrevSeqStr = strconv.FormatInt(prevState.Seq, 10)
	}
	evtPrevDataStr := ""
	if evt.PrevData != nil {
		evtPrevDataStr = ((*cid.Cid)(evt.PrevData)).String()
	}
	newRootCid, err := r.Validator.HandleCommit(ctx, host, account, evt, prevP)
	if err != nil {
		// XXX: induction trace log
		r.Logger.Error("commit bad", "seq", evt.Seq, "pseq", dbPrevSeqStr, "pdsHost", host.Host, "repo", evt.Repo, "prev", evtPrevDataStr, "dbprev", dbPrevRootStr, "err", err)
		r.Logger.Warn("failed handling event", "err", err, "pdsHost", host.Host, "seq", evt.Seq, "repo", account.Did, "commit", evt.Commit.String())
		repoCommitsResultCounter.WithLabelValues(host.Host, "err").Inc()
		return fmt.Errorf("handle user event failed: %w", err)
	} else {
		// store now verified new repo state
		err = r.upsertPrevState(account.ID, newRootCid, evt.Rev, evt.Seq)
		if err != nil {
			return fmt.Errorf("failed to set previous root uid=%d: %w", account.ID, err)
		}
	}

	repoCommitsResultCounter.WithLabelValues(host.Host, "ok").Inc()

	// Broadcast the identity event to all consumers
	commitCopy := *evt
	err = r.Events.AddEvent(ctx, &stream.XRPCStreamEvent{
		RepoCommit: &commitCopy,
		PrivUid:    account.GetUid(),
	})
	if err != nil {
		r.Logger.Error("failed to broadcast commit event", "error", err, "did", evt.Repo)
		return fmt.Errorf("failed to broadcast commit event: %w", err)
	}

	return nil
}

// handleSync processes #sync messages
func (r *Relay) handleSync(ctx context.Context, host *slurper.PDS, evt *comatproto.SyncSubscribeRepos_Sync) error {
	account, err := r.LookupUserByDid(ctx, evt.Did)
	if err != nil {
		if !errors.Is(err, gorm.ErrRecordNotFound) {
			repoCommitsResultCounter.WithLabelValues(host.Host, "nou").Inc()
			return fmt.Errorf("looking up event user: %w", err)
		}

		account, err = r.newUser(ctx, host, evt.Did)
	}
	if err != nil {
		return fmt.Errorf("could not get user for did %#v: %w", evt.Did, err)
	}

	newRootCid, err := r.Validator.HandleSync(ctx, host, evt)
	if err != nil {
		return err
	}
	err = r.upsertPrevState(account.ID, newRootCid, evt.Rev, evt.Seq)
	if err != nil {
		return fmt.Errorf("could not sync set previous state uid=%d: %w", account.ID, err)
	}

	// Broadcast the sync event to all consumers
	evtCopy := *evt
	err = r.Events.AddEvent(ctx, &stream.XRPCStreamEvent{
		RepoSync: &evtCopy,
	})
	if err != nil {
		r.Logger.Error("failed to broadcast sync event", "error", err, "did", evt.Did)
		return fmt.Errorf("failed to broadcast sync event: %w", err)
	}

	return nil
}

func (r *Relay) upsertPrevState(uid uint64, newRootCid *cid.Cid, rev string, seq int64) error {
	cidBytes := newRootCid.Bytes()
	return r.db.Exec(
		"INSERT INTO account_previous_states (uid, cid, rev, seq) VALUES (?, ?, ?, ?) ON CONFLICT (uid) DO UPDATE SET cid = EXCLUDED.cid, rev = EXCLUDED.rev, seq = EXCLUDED.seq",
		uid, cidBytes, rev, seq,
	).Error
}

func (r *Relay) purgeDidCache(ctx context.Context, did string) {
	ati, err := syntax.ParseAtIdentifier(did)
	if err != nil {
		return
	}
	_ = r.dir.Purge(ctx, *ati)
}
