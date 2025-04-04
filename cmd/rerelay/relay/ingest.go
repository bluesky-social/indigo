package relay

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	comatproto "github.com/bluesky-social/indigo/api/atproto"
	"github.com/bluesky-social/indigo/atproto/identity"
	"github.com/bluesky-social/indigo/atproto/syntax"
	"github.com/bluesky-social/indigo/cmd/rerelay/relay/models"
	"github.com/bluesky-social/indigo/cmd/rerelay/stream"

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

// handles the shared part of event processing: that the account existing, is associated with this host, etc
func (r *Relay) preProcessEvent(ctx context.Context, didStr string, hostname string, hostID uint64, logger *slog.Logger) (*models.Account, *identity.Identity, error) {

	did, err := syntax.ParseDID(didStr)
	if err != nil {
		return nil, nil, fmt.Errorf("invalid DID in message: %w", err)
	}
	// XXX: did = did.Normalize()

	acc, err := r.GetAccount(ctx, did)
	if err != nil {
		if !errors.Is(err, ErrAccountNotFound) {
			return nil, nil, fmt.Errorf("fetching account: %w", err)
		}

		acc, err = r.CreateHostAccount(ctx, did, hostID, hostname)
		if err != nil {
			return nil, nil, err
		}
	}

	if acc == nil {
		// TODO: this is defensive and could be removed
		panic(ErrAccountNotFound)
	}

	// verify that the account is on the subscribed host (or update if it should be)
	if err := r.EnsureAccountHost(ctx, acc, hostID, hostname); err != nil {
		return nil, nil, err
	}

	// skip identity lookup if account is not active
	if acc.Status != models.AccountStatusActive || acc.UpstreamStatus != models.AccountStatusActive {
		return acc, nil, nil
	}

	ident, err := r.dir.LookupDID(ctx, did)
	if err != nil {
		// XXX: handle more granularly (eg, true NotFound vs other errors); and add tests
		logger.Warn("failed to load identity")
	}
	return acc, ident, nil
}

func (r *Relay) processCommitEvent(ctx context.Context, evt *comatproto.SyncSubscribeRepos_Commit, hostname string, hostID uint64) error {
	logger := r.Logger.With("did", evt.Repo, "seq", evt.Seq, "host", hostname, "eventType", "commit", "rev", evt.Rev)
	logger.Debug("relay got repo append event")

	acc, ident, err := r.preProcessEvent(ctx, evt.Repo, hostname, hostID, logger)
	if err != nil {
		return err
	}

	if acc.Status != models.AccountStatusActive || acc.UpstreamStatus != models.AccountStatusActive {
		logger.Info("dropping commit message for non-active account", "status", acc.Status, "upstreamStatus", acc.UpstreamStatus)
		return nil
	}

	prevRepo, err := r.GetAccountRepo(ctx, acc.UID)
	if err != nil && !errors.Is(err, ErrAccountRepoNotFound) {
		// TODO: should this be a hard error?
		logger.Error("failed to read previous repo state", "err", err)
	}

	// most commit validation happens in this method. Note that is handles lenient/strict modes.
	newRepo, err := r.VerifyRepoCommit(ctx, evt, ident, prevRepo, hostname)
	if err != nil {
		logger.Warn("commit message failed verification", "err", err)
		return err
	}

	err = r.UpsertAccountRepo(acc.UID, syntax.TID(newRepo.Rev), newRepo.CommitCID, newRepo.CommitData)
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

	acc, ident, err := r.preProcessEvent(ctx, evt.Did, hostname, hostID, logger)
	if err != nil {
		return err
	}

	if acc.Status != models.AccountStatusActive || acc.UpstreamStatus != models.AccountStatusActive {
		logger.Info("dropping commit message for non-active account", "status", acc.Status, "upstreamStatus", acc.UpstreamStatus)
		return nil
	}

	newRepo, err := r.VerifyRepoSync(ctx, evt, ident, hostname)
	if err != nil {
		return err
	}

	err = r.UpsertAccountRepo(acc.UID, syntax.TID(newRepo.Rev), newRepo.CommitCID, newRepo.CommitData)
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

	// TODO: reduce verbosity?
	logger.Info("relay got identity event")

	acc, _, err := r.preProcessEvent(ctx, evt.Did, hostname, hostID, logger)
	if err != nil {
		return err
	}
	did := syntax.DID(acc.DID)

	// Flush any cached DID/identity info for this user
	r.dir.Purge(ctx, did.AtIdentifier())
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

	if evt.Status != nil {
		span.SetAttributes(attribute.String("repo_status", *evt.Status))
	}
	logger.Info("relay got account event")

	if !evt.Active && evt.Status == nil {
		// XXX: what should we do here?
		logger.Warn("invalid account event", "active", evt.Active, "status", evt.Status)
	}

	// Process the upstream account status change
	if err := r.db.Model(models.Account{}).Where("uid = ?", acc.UID).Update("upstream_status", evt.Status).Error; err != nil {
		return err
	}

	// wrangle various status codes in to what is expected in account event
	publicStatus := acc.Status
	if publicStatus == models.AccountStatusActive && evt.Status != nil {
		publicStatus = models.AccountStatus(*evt.Status)
	}
	publicActive := publicStatus == models.AccountStatusActive
	ptrStatus := (*string)(&publicStatus)
	if publicActive {
		ptrStatus = nil
	}

	// emit the event
	err = r.Events.AddEvent(ctx, &stream.XRPCStreamEvent{
		RepoAccount: &comatproto.SyncSubscribeRepos_Account{
			Active: publicActive,
			Did:    acc.DID,
			Status: ptrStatus, // TODO: sometimes will be "active"
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
