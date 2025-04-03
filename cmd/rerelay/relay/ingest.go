package relay

import (
	"context"
	"errors"
	"fmt"
	"time"

	comatproto "github.com/bluesky-social/indigo/api/atproto"
	"github.com/bluesky-social/indigo/atproto/syntax"
	"github.com/bluesky-social/indigo/cmd/rerelay/relay/models"
	"github.com/bluesky-social/indigo/cmd/rerelay/stream"

	"go.opentelemetry.io/otel/attribute"
	"gorm.io/gorm"
)

// This callback function gets called by Slurper on every upstream repo stream message from any host.
//
// Messages are processed in-order for a single account on a single host; but may be concurrent or out-of-order for the same account *across* hosts (eg, during account migration or a conflict)
func (r *Relay) processRepoEvent(ctx context.Context, evt *stream.XRPCStreamEvent, hostname string, hostID uint64) error {
	ctx, span := tracer.Start(ctx, "processRepoEvent")
	defer span.End()

	start := time.Now()
	defer func() {
		eventsHandleDuration.WithLabelValues(hostname).Observe(time.Since(start).Seconds())
	}()

	EventsReceivedCounter.WithLabelValues(hostname).Add(1)

	switch {
	case evt.RepoCommit != nil:
		repoCommitsReceivedCounter.WithLabelValues(hostname).Add(1)
		return r.processCommitEvent(ctx, evt.RepoCommit, hostname, hostID)
	case evt.RepoSync != nil:
		repoSyncReceivedCounter.WithLabelValues(hostname).Add(1)
		return r.processSyncEvent(ctx, evt.RepoSync, hostname, hostID)
	case evt.RepoIdentity != nil:
		//repoIdentityReceivedCounter.WithLabelValues(hostname).Add(1)
		return r.processIdentityEvent(ctx, evt.RepoIdentity, hostname, hostID)
	case evt.RepoAccount != nil:
		//repoAccountReceivedCounter.WithLabelValues(hostname).Add(1)
		return r.processAccountEvent(ctx, evt.RepoAccount, hostname, hostID)
	case evt.RepoHandle != nil: // DEPRECATED
		eventsWarningsCounter.WithLabelValues(hostname, "handle").Add(1)
		return nil
	case evt.RepoMigrate != nil: // DEPRECATED
		eventsWarningsCounter.WithLabelValues(hostname, "migrate").Add(1)
		return nil
	case evt.RepoTombstone != nil: // DEPRECATED
		eventsWarningsCounter.WithLabelValues(hostname, "tombstone").Add(1)
		return nil
	default:
		return fmt.Errorf("unhandled repo stream event type")
	}
}

func (r *Relay) processCommitEvent(ctx context.Context, evt *comatproto.SyncSubscribeRepos_Commit, hostname string, hostID uint64) error {
	logger := r.Logger.With("did", evt.Repo, "seq", evt.Seq, "host", hostname, "eventType", "commit", "rev", evt.Rev)
	logger.Debug("relay got repo append event")

	did, err := syntax.ParseDID(evt.Repo)
	if err != nil {
		return fmt.Errorf("invalid DID in message: %w", err)
	}

	// XXX: did = did.Normalize()
	account, err := r.GetAccount(ctx, did)
	if err != nil {
		if !errors.Is(err, gorm.ErrRecordNotFound) {
			return fmt.Errorf("looking up event user: %w", err)
		}

		host, err := r.GetHost(ctx, hostID)
		if err != nil {
			return err
		}
		account, err = r.CreateAccount(ctx, host, did)
		if err != nil {
			return err
		}
	}

	if account == nil {
		return ErrAccountNotFound
	}

	// XXX: lock on account
	ustatus := account.UpstreamStatus

	// XXX: lock on account
	if account.Status == models.AccountStatusTakendown || ustatus == models.AccountStatusTakendown {
		logger.Debug("dropping commit event from taken down user")
		return nil
	}

	if ustatus == models.AccountStatusSuspended {
		logger.Debug("dropping commit event from suspended user")
		return nil
	}

	if ustatus == models.AccountStatusDeactivated {
		logger.Debug("dropping commit event from deactivated user")
		return nil
	}

	if evt.Rebase {
		return fmt.Errorf("rebase was true in event seq:%d,host:%s", evt.Seq, hostname)
	}

	accountHostId := account.HostID
	if hostID != accountHostId && accountHostId != 0 {
		// XXX: metter logging
		logger.Warn("received event for repo from different pds than expected", "expectedHostID", accountHostId, "receivedHost", hostname)
		// Flush any cached DID documents for this user
		err = r.dir.Purge(ctx, did.AtIdentifier())
		if err != nil {
			logger.Error("problem purging identity directory cache", "err", err)
		}

		// XXX: shouldn't need full Host?
		host, err := r.GetHost(ctx, hostID)
		if err != nil {
			return err
		}

		account, err = r.syncHostAccount(ctx, did, host, account)
		if err != nil {
			return err
		}

		if account.HostID != hostID && !r.Config.SkipAccountHostCheck {
			return fmt.Errorf("event from non-authoritative pds")
		}
	}

	ident, err := r.dir.LookupDID(ctx, did)
	if err != nil {
		// XXX: handle more granularly (eg, true not-founds vs errors); and add tests
		logger.Warn("failed to load identity")
	}

	var prevRepo *models.AccountRepo
	err = r.db.First(prevRepo, account.UID).Error
	if err != nil {
		if !errors.Is(err, gorm.ErrRecordNotFound) {
			// TODO: should this be a hard error?
			logger.Error("failed to read previous repo state", "err", err)
		}
		prevRepo = nil
	}

	// most commit validation happens in this method. Note that is handles lenient/strict modes.
	newRepo, err := r.VerifyRepoCommit(ctx, evt, ident, prevRepo, hostname)
	if err != nil {
		logger.Warn("commit message failed verification", "err", err)
		return err
	}

	err = r.UpsertAccountRepo(account.UID, syntax.TID(newRepo.Rev), newRepo.CommitCID, newRepo.CommitData)
	if err != nil {
		return fmt.Errorf("failed to upsert account repo (%s): %w", account.DID, err)
	}

	// Broadcast the identity event to all consumers
	// TODO: is this copy important?
	commitCopy := *evt
	err = r.Events.AddEvent(ctx, &stream.XRPCStreamEvent{
		RepoCommit: &commitCopy,
		PrivUid:    account.UID,
	})
	if err != nil {
		logger.Error("failed to broadcast commit event", "error", err)
		return fmt.Errorf("failed to broadcast commit event: %w", err)
	}

	return nil
}

func (r *Relay) processSyncEvent(ctx context.Context, evt *comatproto.SyncSubscribeRepos_Sync, hostname string, hostID uint64) error {
	logger := r.Logger.With("did", evt.Did, "seq", evt.Seq, "host", hostname, "eventType", "sync")
	did, err := syntax.ParseDID(evt.Did)
	if err != nil {
		return fmt.Errorf("invalid DID in message: %s", did)
	}
	// XXX: did.Normalize()
	account, err := r.GetAccount(ctx, did)
	if err != nil {
		if !errors.Is(err, gorm.ErrRecordNotFound) {
			return fmt.Errorf("looking up event user: %w", err)
		}

		host, err := r.GetHost(ctx, hostID)
		if err != nil {
			return err
		}
		account, err = r.CreateAccount(ctx, host, did)
	}
	if err != nil {
		return fmt.Errorf("could not get user for did %#v: %w", evt.Did, err)
	}

	ident, err := r.dir.LookupDID(ctx, did)
	if err != nil {
		// XXX: handle more granularly (eg, true not-founds vs errors); and add tests
		logger.Warn("failed to load identity")
	}

	newRepo, err := r.VerifyRepoSync(ctx, evt, ident, hostname)
	if err != nil {
		return err
	}

	// TODO: should this happen before or after firehose persist/broadcast?
	err = r.UpsertAccountRepo(account.UID, syntax.TID(newRepo.Rev), newRepo.CommitCID, newRepo.CommitData)
	if err != nil {
		return fmt.Errorf("failed to upsert repo state (uid %d): %w", account.UID, err)
	}

	// Broadcast the sync event to all consumers
	evtCopy := *evt
	err = r.Events.AddEvent(ctx, &stream.XRPCStreamEvent{
		RepoSync: &evtCopy,
	})
	if err != nil {
		logger.Error("failed to broadcast sync event", "error", err)
		return fmt.Errorf("failed to broadcast sync event: %w", err)
	}

	return nil
}

func (r *Relay) processIdentityEvent(ctx context.Context, evt *comatproto.SyncSubscribeRepos_Identity, hostname string, hostID uint64) error {
	logger := r.Logger.With("did", evt.Did, "seq", evt.Seq, "host", hostname, "eventType", "identity")
	logger.Info("relay got identity event")

	did, err := syntax.ParseDID(evt.Did)
	if err != nil {
		return fmt.Errorf("invalid DID in message: %w", err)
	}

	// Flush any cached DID documents for this user
	r.dir.Purge(ctx, did.AtIdentifier())
	if err != nil {
		logger.Error("problem purging identity directory cache", "err", err)
	}

	// XXX: syncHostAccount doesn't need full Host?
	host, err := r.GetHost(ctx, hostID)
	if err != nil {
		return err
	}

	// Refetch the DID doc and update our cached keys and handle etc.
	account, err := r.syncHostAccount(ctx, did, host, nil)
	if err != nil {
		return err
	}

	// Broadcast the identity event to all consumers
	err = r.Events.AddEvent(ctx, &stream.XRPCStreamEvent{
		RepoIdentity: &comatproto.SyncSubscribeRepos_Identity{
			Did:    did.String(),
			Seq:    evt.Seq,
			Time:   evt.Time,
			Handle: evt.Handle,
		},
		PrivUid: account.UID,
	})
	if err != nil {
		logger.Error("failed to broadcast Identity event", "error", err)
		return fmt.Errorf("failed to broadcast Identity event: %w", err)
	}

	return nil
}

func (r *Relay) processAccountEvent(ctx context.Context, evt *comatproto.SyncSubscribeRepos_Account, hostname string, hostID uint64) error {
	logger := r.Logger.With("did", evt.Did, "seq", evt.Seq, "host", hostname, "eventType", "account")

	ctx, span := tracer.Start(ctx, "processAccountEvent")
	defer span.End()
	span.SetAttributes(
		attribute.String("did", evt.Did),
		attribute.Int64("seq", evt.Seq),
		attribute.Bool("active", evt.Active),
	)

	did, err := syntax.ParseDID(evt.Did)
	if err != nil {
		return fmt.Errorf("invalid DID in message: %w", err)
	}

	if evt.Status != nil {
		span.SetAttributes(attribute.String("repo_status", *evt.Status))
	}
	logger.Info("relay got account event")

	if !evt.Active && evt.Status == nil {
		// TODO: semantics here aren't really clear
		logger.Warn("dropping invalid account event", "active", evt.Active, "status", evt.Status)
		accountVerifyWarnings.WithLabelValues(hostname, "nostat").Inc()
		return nil
	}

	// Flush any cached DID documents for this user
	r.dir.Purge(ctx, did.AtIdentifier())
	if err != nil {
		logger.Error("problem purging identity directory cache", "err", err)
	}

	// XXX: shouldn't need full host?
	host, err := r.GetHost(ctx, hostID)
	if err != nil {
		return err
	}

	// Refetch the DID doc to make sure the Host is still authoritative
	account, err := r.syncHostAccount(ctx, did, host, nil)
	if err != nil {
		span.RecordError(err)
		return err
	}

	// Check if the Host is still authoritative
	// if not we don't want to be propagating this account event
	// XXX: lock
	if account.HostID != hostID && !r.Config.SkipAccountHostCheck {
		logger.Error("account event from non-authoritative pds",
			"event_from", hostname,
			"did_doc_declared_pds", account.HostID,
			"account_evt", evt,
		)
		return fmt.Errorf("event from non-authoritative pds")
	}

	// Process the account status change
	repoStatus := models.AccountStatusActive
	if !evt.Active && evt.Status != nil {
		repoStatus = models.AccountStatus(*evt.Status)
	}

	// XXX: lock, and parse
	account.UpstreamStatus = models.AccountStatus(repoStatus)
	err = r.db.Save(account).Error
	if err != nil {
		span.RecordError(err)
		return fmt.Errorf("failed to update account status: %w", err)
	}

	shouldBeActive := evt.Active
	status := evt.Status

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
			Did:    evt.Did,
			Seq:    evt.Seq,
			Status: status,
			Time:   evt.Time,
		},
		PrivUid: account.UID,
	})
	if err != nil {
		logger.Error("failed to broadcast Account event", "error", err)
		return fmt.Errorf("failed to broadcast Account event: %w", err)
	}

	return nil
}
