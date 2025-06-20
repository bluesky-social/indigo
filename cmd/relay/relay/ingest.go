package relay

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	comatproto "github.com/gander-social/gander-indigo-sovereign/api/atproto"
	"github.com/gander-social/gander-indigo-sovereign/atproto/identity"
	"github.com/gander-social/gander-indigo-sovereign/atproto/syntax"
	"github.com/gander-social/gander-indigo-sovereign/cmd/relay/relay/models"
	"github.com/gander-social/gander-indigo-sovereign/cmd/relay/stream"

	"go.opentelemetry.io/otel/attribute"
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
	default:
		return fmt.Errorf("unhandled repo stream event type")
	}
}

// Implements the shared part of event processing: that the account existing, is associated with this host, etc.
//
// If there is no error, the returned account is always non-nil, but the identity may be nil (if there was a resolution error).
func (r *Relay) preProcessEvent(ctx context.Context, didStr string, hostname string, hostID uint64, logger *slog.Logger) (*models.Account, *identity.Identity, error) {

	did, err := syntax.ParseDID(didStr)
	if err != nil {
		return nil, nil, fmt.Errorf("invalid DID in message: %w", err)
	}
	// TODO: add a test case for non-normalized DID
	did = NormalizeDID(did)

	acc, err := r.GetAccount(ctx, did)
	if err != nil {
		if !errors.Is(err, ErrAccountNotFound) {
			return nil, nil, fmt.Errorf("fetching account: %w", err)
		}

		acc, err = r.CreateAccountHost(ctx, did, hostID, hostname)
		if err != nil {
			return nil, nil, err
		}
	}

	// verify that the account is on the subscribed host (or update if it should be)
	if err := r.EnsureAccountHost(ctx, acc, hostID, hostname); err != nil {
		return nil, nil, err
	}

	// skip identity lookup if account is not active
	if !acc.IsActive() {
		return acc, nil, nil
	}

	ident, err := r.Dir.LookupDID(ctx, did)
	if err != nil {
		logger.Warn("failed to load identity", "did", did, "err", err)
	}
	return acc, ident, nil
}

func (r *Relay) processCommitEvent(ctx context.Context, evt *comatproto.SyncSubscribeRepos_Commit, hostname string, hostID uint64) error {
	logger := r.Logger.With("did", evt.Repo, "seq", evt.Seq, "host", hostname, "eventType", "commit", "rev", evt.Rev)
	logger.Debug("relay got commit event")

	acc, ident, err := r.preProcessEvent(ctx, evt.Repo, hostname, hostID, logger)
	if err != nil {
		return err
	}

	if !acc.IsActive() {
		logger.Info("dropping commit message for non-active account", "status", acc.Status, "upstreamStatus", acc.UpstreamStatus)
		return nil
	}

	if ident == nil {
		// TODO: what to do if identity resolution fails
	}

	prevRepo, err := r.GetAccountRepo(ctx, acc.UID)
	if err != nil && !errors.Is(err, ErrAccountRepoNotFound) {
		logger.Error("failed to read previous repo state", "err", err)
		return err
	}

	// fast check for stale revision (will be re-checked in VerifyRepoCommit)
	if prevRepo != nil && prevRepo.Rev != "" && evt.Rev != "" {
		if evt.Rev <= prevRepo.Rev {
			logger.Warn("dropping commit with old rev", "prevRev", prevRepo.Rev)
			return nil
		}
	}

	// most commit validation happens in this method. Note that is handles lenient/strict modes.
	newRepo, err := r.VerifyRepoCommit(ctx, evt, ident, prevRepo, hostname)
	if err != nil {
		logger.Warn("commit message failed verification", "err", err)
		return err
	}

	err = r.UpsertAccountRepo(ctx, acc.UID, syntax.TID(newRepo.Rev), newRepo.CommitCID, newRepo.CommitDataCID)
	if err != nil {
		return fmt.Errorf("failed to upsert account repo (%s): %w", acc.DID, err)
	}

	// emit the event
	// TODO: is this copy important?
	commitCopy := *evt
	err = r.Events.AddEvent(ctx, &stream.XRPCStreamEvent{
		RepoCommit: &commitCopy,
		PrivUid:    acc.UID,
	})
	if err != nil {
		logger.Error("failed to broadcast event", "error", err)
		return fmt.Errorf("failed to broadcast #commit event: %w", err)
	}

	return nil
}

func (r *Relay) processSyncEvent(ctx context.Context, evt *comatproto.SyncSubscribeRepos_Sync, hostname string, hostID uint64) error {
	logger := r.Logger.With("did", evt.Did, "seq", evt.Seq, "host", hostname, "eventType", "sync")
	logger.Debug("relay got sync event")

	acc, ident, err := r.preProcessEvent(ctx, evt.Did, hostname, hostID, logger)
	if err != nil {
		return err
	}

	if !acc.IsActive() {
		logger.Info("dropping sync message for non-active account", "status", acc.Status, "upstreamStatus", acc.UpstreamStatus)
		return nil
	}

	if ident == nil {
		// TODO: what to do if identity resolution fails
	}

	// TODO: should we load account 'rev' here and prevent roll-backs? or allow roll-backs?

	newRepo, err := r.VerifyRepoSync(ctx, evt, ident, hostname)
	if err != nil {
		return err
	}

	err = r.UpsertAccountRepo(ctx, acc.UID, syntax.TID(newRepo.Rev), newRepo.CommitCID, newRepo.CommitDataCID)
	if err != nil {
		return fmt.Errorf("failed to upsert account repo (%s): %w", acc.DID, err)
	}

	// emit the event
	evtCopy := *evt
	err = r.Events.AddEvent(ctx, &stream.XRPCStreamEvent{
		RepoSync: &evtCopy,
		PrivUid:  acc.UID,
	})
	if err != nil {
		logger.Error("failed to broadcast event", "error", err)
		return fmt.Errorf("failed to broadcast #sync event: %w", err)
	}
	return nil
}

func (r *Relay) processIdentityEvent(ctx context.Context, evt *comatproto.SyncSubscribeRepos_Identity, hostname string, hostID uint64) error {
	logger := r.Logger.With("did", evt.Did, "seq", evt.Seq, "host", hostname, "eventType", "identity")
	logger.Debug("relay got identity event")

	acc, _, err := r.preProcessEvent(ctx, evt.Did, hostname, hostID, logger)
	if err != nil {
		return err
	}
	did := syntax.DID(acc.DID)

	// Flush any cached DID/identity info for this user
	err = r.Dir.Purge(ctx, did.AtIdentifier())
	if err != nil {
		logger.Error("problem purging identity directory cache", "err", err)
	}

	// Broadcast the identity event to all consumers
	err = r.Events.AddEvent(ctx, &stream.XRPCStreamEvent{
		RepoIdentity: &comatproto.SyncSubscribeRepos_Identity{
			Did:    did.String(),
			Time:   evt.Time,   // TODO: update to now?
			Handle: evt.Handle, // TODO: we could substitute in our handle resolution here
		},
		PrivUid: acc.UID,
	})
	if err != nil {
		logger.Error("failed to broadcast identity event", "error", err)
		return fmt.Errorf("failed to broadcast #identity event: %w", err)
	}

	return nil
}

func (r *Relay) processAccountEvent(ctx context.Context, evt *comatproto.SyncSubscribeRepos_Account, hostname string, hostID uint64) error {
	logger := r.Logger.With("did", evt.Did, "seq", evt.Seq, "host", hostname, "eventType", "account")
	logger.Debug("relay got account event")

	ctx, span := tracer.Start(ctx, "processAccountEvent")
	defer span.End()
	span.SetAttributes(
		attribute.String("did", evt.Did),
		attribute.Int64("seq", evt.Seq),
		attribute.Bool("active", evt.Active),
	)

	acc, _, err := r.preProcessEvent(ctx, evt.Did, hostname, hostID, logger)
	if err != nil {
		return err
	}

	if !evt.Active && evt.Status == nil {
		logger.Warn("invalid account event", "active", evt.Active, "status", evt.Status)
	}

	newStatus := models.AccountStatusInactive
	if evt.Active {
		newStatus = models.AccountStatusActive
	} else if evt.Status != nil {
		newStatus = models.AccountStatus(*evt.Status)
	}

	if newStatus != acc.UpstreamStatus {
		if err := r.UpdateAccountUpstreamStatus(ctx, syntax.DID(acc.DID), acc.UID, newStatus); err != nil {
			return err
		}
		acc.UpstreamStatus = newStatus
	}

	// emit the event
	err = r.Events.AddEvent(ctx, &stream.XRPCStreamEvent{
		RepoAccount: &comatproto.SyncSubscribeRepos_Account{
			Active: acc.IsActive(),
			Did:    acc.DID,
			Status: acc.StatusField(),
			Time:   evt.Time,
		},
		PrivUid: acc.UID,
	})
	if err != nil {
		logger.Error("failed to broadcast event", "error", err)
		return fmt.Errorf("failed to broadcast #account event: %w", err)
	}

	return nil
}
