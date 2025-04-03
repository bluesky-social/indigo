package relay

import (
	"context"
	"errors"
	"fmt"
	"time"

	comatproto "github.com/bluesky-social/indigo/api/atproto"
	"github.com/bluesky-social/indigo/atproto/syntax"
	"github.com/bluesky-social/indigo/cmd/relayered/relay/models"
	"github.com/bluesky-social/indigo/cmd/relayered/stream"

	"github.com/ipfs/go-cid"
	"go.opentelemetry.io/otel/attribute"
	"gorm.io/gorm"
)

// handleFedEvent() is the callback passed to Slurper called from Slurper.handleConnection()
// XXX: evt not env
func (r *Relay) handleFedEvent(ctx context.Context, host *models.Host, env *stream.XRPCStreamEvent) error {
	ctx, span := tracer.Start(ctx, "handleFedEvent")
	defer span.End()

	start := time.Now()
	defer func() {
		eventsHandleDuration.WithLabelValues(host.Hostname).Observe(time.Since(start).Seconds())
	}()

	EventsReceivedCounter.WithLabelValues(host.Hostname).Add(1)

	switch {
	case env.RepoCommit != nil:
		repoCommitsReceivedCounter.WithLabelValues(host.Hostname).Add(1)
		return r.handleCommit(ctx, host, env.RepoCommit)
	case env.RepoSync != nil:
		repoSyncReceivedCounter.WithLabelValues(host.Hostname).Add(1)
		return r.handleSync(ctx, host, env.RepoSync)
	case env.RepoHandle != nil:
		eventsWarningsCounter.WithLabelValues(host.Hostname, "handle").Add(1)
		// TODO: rate limit warnings per Host before we (temporarily?) block them
		return nil
	case env.RepoIdentity != nil:
		r.Logger.Info("relay got identity event", "did", env.RepoIdentity.Did)

		did, err := syntax.ParseDID(env.RepoIdentity.Did)
		if err != nil {
			return fmt.Errorf("invalid DID in message: %w", err)
		}

		// Flush any cached DID documents for this user
		r.purgeDidCache(ctx, did.String())

		// Refetch the DID doc and update our cached keys and handle etc.
		account, err := r.syncHostAccount(ctx, did, host, nil)
		if err != nil {
			return err
		}

		// Broadcast the identity event to all consumers
		err = r.Events.AddEvent(ctx, &stream.XRPCStreamEvent{
			RepoIdentity: &comatproto.SyncSubscribeRepos_Identity{
				Did:    did.String(),
				Seq:    env.RepoIdentity.Seq,
				Time:   env.RepoIdentity.Time,
				Handle: env.RepoIdentity.Handle,
			},
			PrivUid: account.UID,
		})
		if err != nil {
			r.Logger.Error("failed to broadcast Identity event", "error", err, "did", did)
			return fmt.Errorf("failed to broadcast Identity event: %w", err)
		}

		return nil
	case env.RepoAccount != nil:
		span.SetAttributes(
			attribute.String("did", env.RepoAccount.Did),
			attribute.Int64("seq", env.RepoAccount.Seq),
			attribute.Bool("active", env.RepoAccount.Active),
		)

		did, err := syntax.ParseDID(env.RepoAccount.Did)
		if err != nil {
			return fmt.Errorf("invalid DID in message: %w", err)
		}

		if env.RepoAccount.Status != nil {
			span.SetAttributes(attribute.String("repo_status", *env.RepoAccount.Status))
		}
		r.Logger.Info("relay got account event", "did", env.RepoAccount.Did)

		if !env.RepoAccount.Active && env.RepoAccount.Status == nil {
			// TODO: semantics here aren't really clear
			r.Logger.Warn("dropping invalid account event", "did", env.RepoAccount.Did, "active", env.RepoAccount.Active, "status", env.RepoAccount.Status)
			accountVerifyWarnings.WithLabelValues(host.Hostname, "nostat").Inc()
			return nil
		}

		// Flush any cached DID documents for this user
		r.purgeDidCache(ctx, did.String())

		// Refetch the DID doc to make sure the Host is still authoritative
		account, err := r.syncHostAccount(ctx, did, host, nil)
		if err != nil {
			span.RecordError(err)
			return err
		}

		// Check if the Host is still authoritative
		// if not we don't want to be propagating this account event
		// XXX: lock
		if account.HostID != host.ID && !r.Config.SkipAccountHostCheck {
			r.Logger.Error("account event from non-authoritative pds",
				"seq", env.RepoAccount.Seq,
				"did", env.RepoAccount.Did,
				"event_from", host.Hostname,
				"did_doc_declared_pds", account.HostID,
				"account_evt", env.RepoAccount,
			)
			return fmt.Errorf("event from non-authoritative pds")
		}

		// Process the account status change
		repoStatus := models.AccountStatusActive
		if !env.RepoAccount.Active && env.RepoAccount.Status != nil {
			repoStatus = models.AccountStatus(*env.RepoAccount.Status)
		}

		// XXX: lock, and parse
		account.UpstreamStatus = models.AccountStatus(repoStatus)
		err = r.db.Save(account).Error
		if err != nil {
			span.RecordError(err)
			return fmt.Errorf("failed to update account status: %w", err)
		}

		shouldBeActive := env.RepoAccount.Active
		status := env.RepoAccount.Status

		// override with local status
		// XXX: lock
		if account.Status == "takendown" {
			shouldBeActive = false
			s := string(models.AccountStatusTakendown)
			status = &s
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
			PrivUid: account.UID,
		})
		if err != nil {
			r.Logger.Error("failed to broadcast Account event", "error", err, "did", env.RepoAccount.Did)
			return fmt.Errorf("failed to broadcast Account event: %w", err)
		}

		return nil
	case env.RepoMigrate != nil:
		eventsWarningsCounter.WithLabelValues(host.Hostname, "migrate").Add(1)
		// TODO: rate limit warnings per Host before we (temporarily?) block them
		return nil
	case env.RepoTombstone != nil:
		eventsWarningsCounter.WithLabelValues(host.Hostname, "tombstone").Add(1)
		// TODO: rate limit warnings per Host before we (temporarily?) block them
		return nil
	default:
		return fmt.Errorf("invalid fed event")
	}
}

func (r *Relay) handleCommit(ctx context.Context, host *models.Host, evt *comatproto.SyncSubscribeRepos_Commit) error {
	r.Logger.Debug("relay got repo append event", "seq", evt.Seq, "host", host.Hostname, "repo", evt.Repo)

	did, err := syntax.ParseDID(evt.Repo)
	if err != nil {
		return fmt.Errorf("invalid DID in message: %w", err)
	}
	// XXX: did = did.Normalize()
	account, err := r.GetAccount(ctx, did)
	if err != nil {
		if !errors.Is(err, gorm.ErrRecordNotFound) {
			repoCommitsResultCounter.WithLabelValues(host.Hostname, "nou").Inc()
			return fmt.Errorf("looking up event user: %w", err)
		}

		account, err = r.CreateAccount(ctx, host, did)
		if err != nil {
			repoCommitsResultCounter.WithLabelValues(host.Hostname, "nuerr").Inc()
			return err
		}
	}
	if account == nil {
		repoCommitsResultCounter.WithLabelValues(host.Hostname, "nou2").Inc()
		return ErrAccountNotFound
	}

	// XXX: lock on account
	ustatus := account.UpstreamStatus

	// XXX: lock on account
	if account.Status == models.AccountStatusTakendown || ustatus == models.AccountStatusTakendown {
		r.Logger.Debug("dropping commit event from taken down user", "did", evt.Repo, "seq", evt.Seq, "host", host.Hostname)
		repoCommitsResultCounter.WithLabelValues(host.Hostname, "tdu").Inc()
		return nil
	}

	if ustatus == models.AccountStatusSuspended {
		r.Logger.Debug("dropping commit event from suspended user", "did", evt.Repo, "seq", evt.Seq, "host", host.Hostname)
		repoCommitsResultCounter.WithLabelValues(host.Hostname, "susu").Inc()
		return nil
	}

	if ustatus == models.AccountStatusDeactivated {
		r.Logger.Debug("dropping commit event from deactivated user", "did", evt.Repo, "seq", evt.Seq, "host", host.Hostname)
		repoCommitsResultCounter.WithLabelValues(host.Hostname, "du").Inc()
		return nil
	}

	if evt.Rebase {
		repoCommitsResultCounter.WithLabelValues(host.Hostname, "rebase").Inc()
		return fmt.Errorf("rebase was true in event seq:%d,host:%s", evt.Seq, host.Hostname)
	}

	accountHostId := account.HostID
	if host.ID != accountHostId && accountHostId != 0 {
		r.Logger.Warn("received event for repo from different pds than expected", "repo", evt.Repo, "expPds", accountHostId, "gotPds", host.Hostname)
		// Flush any cached DID documents for this user
		r.purgeDidCache(ctx, evt.Repo)

		account, err = r.syncHostAccount(ctx, did, host, account)
		if err != nil {
			repoCommitsResultCounter.WithLabelValues(host.Hostname, "uerr2").Inc()
			return err
		}

		if account.HostID != host.ID && !r.Config.SkipAccountHostCheck {
			repoCommitsResultCounter.WithLabelValues(host.Hostname, "noauth").Inc()
			return fmt.Errorf("event from non-authoritative pds")
		}
	}

	// TODO: very messy fetch code here
	var repo *models.AccountRepo
	err = r.db.First(repo, account.UID).Error
	if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
		r.Logger.Error("failed to get previous root", "err", err)
		repo = nil
	}
	var prevRev *syntax.TID
	var prevData *cid.Cid
	if repo != nil {
		// XXX: repo.CommitData
		//prevData = &repo.Cid.CID
		t := syntax.TID(repo.Rev)
		prevRev = &t
	}
	evtPrevDataStr := ""
	if evt.PrevData != nil {
		evtPrevDataStr = ((*cid.Cid)(evt.PrevData)).String()
	}
	newRootCid, err := r.Validator.HandleCommit(ctx, host, account, evt, prevRev, prevData)
	if err != nil {
		// XXX: induction trace log
		r.Logger.Error("commit bad", "seq", evt.Seq, "host", host.Hostname, "repo", evt.Repo, "prev", evtPrevDataStr, "err", err)
		r.Logger.Warn("failed handling event", "err", err, "host", host.Hostname, "seq", evt.Seq, "repo", account.DID, "commit", evt.Commit.String())
		repoCommitsResultCounter.WithLabelValues(host.Hostname, "err").Inc()
		return fmt.Errorf("handle user event failed: %w", err)
	} else {
		// store now verified new repo state
		err = r.upsertPrevState(account.UID, newRootCid, evt.Rev, evt.Seq)
		if err != nil {
			return fmt.Errorf("failed to set previous root uid=%d: %w", account.UID, err)
		}
	}

	repoCommitsResultCounter.WithLabelValues(host.Hostname, "ok").Inc()

	// Broadcast the identity event to all consumers
	commitCopy := *evt
	err = r.Events.AddEvent(ctx, &stream.XRPCStreamEvent{
		RepoCommit: &commitCopy,
		PrivUid:    account.UID,
	})
	if err != nil {
		r.Logger.Error("failed to broadcast commit event", "error", err, "did", evt.Repo)
		return fmt.Errorf("failed to broadcast commit event: %w", err)
	}

	return nil
}

// handleSync processes #sync messages
func (r *Relay) handleSync(ctx context.Context, host *models.Host, evt *comatproto.SyncSubscribeRepos_Sync) error {
	did, err := syntax.ParseDID(evt.Did)
	if err != nil {
		return fmt.Errorf("invalid DID in message: %s", did)
	}
	// XXX: did.Normalize()
	account, err := r.GetAccount(ctx, did)
	if err != nil {
		if !errors.Is(err, gorm.ErrRecordNotFound) {
			repoCommitsResultCounter.WithLabelValues(host.Hostname, "nou").Inc()
			return fmt.Errorf("looking up event user: %w", err)
		}

		account, err = r.CreateAccount(ctx, host, did)
	}
	if err != nil {
		return fmt.Errorf("could not get user for did %#v: %w", evt.Did, err)
	}

	newRootCid, err := r.Validator.HandleSync(ctx, host, evt)
	if err != nil {
		return err
	}
	err = r.upsertPrevState(account.UID, newRootCid, evt.Rev, evt.Seq)
	if err != nil {
		return fmt.Errorf("could not sync set previous state uid=%d: %w", account.UID, err)
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
	// XXX: which is this actually
	return r.db.Exec(
		"INSERT INTO account_repo (uid, rev, commit_data) VALUES (?, ?, ?) ON CONFLICT (uid) DO UPDATE SET commit_data = EXCLUDED.commit_data , rev = EXCLUDED.rev",
		uid, rev, newRootCid.String(),
	).Error
}

func (r *Relay) purgeDidCache(ctx context.Context, did string) {
	ati, err := syntax.ParseAtIdentifier(did)
	if err != nil {
		return
	}
	_ = r.dir.Purge(ctx, *ati)
}
