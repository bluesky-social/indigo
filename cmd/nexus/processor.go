package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"sync/atomic"
	"time"

	comatproto "github.com/bluesky-social/indigo/api/atproto"
	"github.com/bluesky-social/indigo/atproto/atdata"
	"github.com/bluesky-social/indigo/atproto/identity"
	"github.com/bluesky-social/indigo/atproto/repo"
	"github.com/bluesky-social/indigo/atproto/syntax"
	"github.com/bluesky-social/indigo/cmd/nexus/models"
	"go.opentelemetry.io/otel"
	"gorm.io/gorm"
)

var tracer = otel.Tracer("nexus")

type EventProcessor struct {
	Logger   *slog.Logger
	DB       *gorm.DB
	Dir      identity.Directory
	RelayUrl string
	Events   *EventManager

	FullNetworkMode   bool
	SignalCollection  string
	CollectionFilters []string

	lastSeq atomic.Int64
}

// ProcessCommit validates and applies a commit event from the firehose.
func (ep *EventProcessor) ProcessCommit(ctx context.Context, evt *comatproto.SyncSubscribeRepos_Commit) error {
	ctx, span := tracer.Start(ctx, "ProcessCommit")
	defer span.End()

	defer ep.lastSeq.Store(evt.Seq)

	curr, err := ep.GetRepoState(evt.Repo)
	if err != nil {
		return err
	} else if curr == nil {
		shouldTrack := ep.FullNetworkMode || (ep.SignalCollection != "" && evtHasSignalCollection(evt, ep.SignalCollection))
		if shouldTrack {
			if err := ep.EnsureRepo(evt.Repo); err != nil {
				ep.Logger.Error("failed to auto-track repo", "did", evt.Repo, "error", err)
				return err
			}
		}
		// even if we just tracked, we return here and just let the resync workers handle the rest
		return nil
	}

	if curr.State != models.RepoStateActive && curr.State != models.RepoStateResyncing {
		return nil
	}

	if curr.Rev != "" && evt.Rev <= curr.Rev {
		ep.Logger.Debug("skipping replayed event", "did", evt.Repo, "eventRev", evt.Rev, "currentRev", curr.Rev)
		return nil
	}

	if evt.PrevData == nil {
		ep.Logger.Debug("legacy commit event, skipping prev data check", "did", evt.Repo, "rev", evt.Rev)
	} else if evt.PrevData.String() != curr.PrevData {
		ep.Logger.Warn("repo state desynced", "did", evt.Repo, "rev", evt.Rev)
		// gets picked up by resync workers
		if err := ep.UpdateRepoState(evt.Repo, models.RepoStateDesynced); err != nil {
			ep.Logger.Error("failed to update repo state to desynced", "did", evt.Repo, "error", err)
			return err
		}
		return nil
	}

	commit, err := ep.validateCommit(ctx, evt)
	if err != nil {
		ep.Logger.Error("failed to parse operations", "did", evt.Repo, "error", err)
		return err
	}

	// filter ops to only matching collections after validation (since all ops are necessary for commit validation)
	filteredOps := []CommitOp{}
	for _, op := range commit.Ops {
		if matchesCollection(op.Collection, ep.CollectionFilters) {
			filteredOps = append(filteredOps, op)
		}
	}
	if len(filteredOps) == 0 {
		return nil
	}
	commit.Ops = filteredOps

	if curr.State == models.RepoStateResyncing {
		if err := ep.addToResyncBuffer(commit); err != nil {
			ep.Logger.Error("failed to buffer commit", "did", evt.Repo, "error", err)
			return err
		}
	}

	if err := ep.Events.AddCommit(commit, func(tx *gorm.DB) error {
		return nil
	}); err != nil {
		ep.Logger.Error("failed to update repo state", "did", commit.Did, "rev", commit.Rev, "error", err)
		return err
	}

	return nil
}

func (ep *EventProcessor) validateCommit(ctx context.Context, evt *comatproto.SyncSubscribeRepos_Commit) (*Commit, error) {
	if err := repo.VerifyCommitSignature(ctx, ep.Dir, evt); err != nil {
		return nil, err
	}

	r, err := repo.VerifyCommitMessage(ctx, evt)
	if err != nil {
		return nil, err
	}

	var parsedOps []CommitOp

	for _, op := range evt.Ops {
		collection, rkey, err := syntax.ParseRepoPath(op.Path)
		if err != nil {
			return nil, fmt.Errorf("invalid record path: %w", err)
		}

		parsed := CommitOp{
			Collection: collection.String(),
			Rkey:       rkey.String(),
			Action:     op.Action,
		}

		if op.Action == "create" || op.Action == "update" {
			if op.Cid == nil {
				return nil, fmt.Errorf("missing CID for create/update: %s", op.Path)
			}
			parsed.Cid = op.Cid.String()

			recBytes, _, err := r.GetRecordBytes(ctx, collection, rkey)
			if err != nil {
				ep.Logger.Error("failed to get record bytes", "did", evt.Repo, "path", op.Path, "error", err)
				continue
			}

			record, err := atdata.UnmarshalCBOR(recBytes)
			if err != nil {
				// do not skip here
				// we end up storing the CID but not passing the record along in the outbox
				ep.Logger.Error("failed to unmarshal record", "did", evt.Repo, "path", op.Path, "error", err)
			}
			parsed.Record = record
		}

		parsedOps = append(parsedOps, parsed)
	}

	repoCommit, err := r.Commit()
	if err != nil {
		return nil, err
	}

	commit := &Commit{
		Did:     evt.Repo,
		Rev:     repoCommit.Rev,
		DataCid: repoCommit.Data.String(),
		Ops:     parsedOps,
	}

	return commit, nil
}

// ProcessSync handles sync events and marks repos for resync if needed.
func (ep *EventProcessor) ProcessSync(ctx context.Context, evt *comatproto.SyncSubscribeRepos_Sync) error {
	ctx, span := tracer.Start(ctx, "ProcessSync")
	defer span.End()

	defer ep.lastSeq.Store(evt.Seq)

	curr, err := ep.GetRepoState(evt.Did)
	if err != nil {
		return err
	} else if curr == nil {
		if ep.FullNetworkMode {
			if err := ep.EnsureRepo(evt.Did); err != nil {
				ep.Logger.Error("failed to auto-track repo", "did", evt.Did, "error", err)
				return err
			}
			return nil
		}
		return nil
	}

	commit, err := repo.VerifySyncMessage(ctx, ep.Dir, evt)
	if err != nil {
		return fmt.Errorf("failed to verify sync message: %w", err)
	}

	if curr.State != models.RepoStateActive {
		return nil
	}

	if curr.Rev != "" && commit.Rev <= curr.Rev {
		ep.Logger.Debug("skipping replayed event", "did", commit.DID, "eventRev", commit.Rev, "currentRev", curr.Rev)
		return nil
	}

	if curr.PrevData == commit.Data.String() {
		ep.Logger.Debug("skipping noop sync event", "did", commit.DID, "rev", commit.Rev)
		return nil
	}

	if err := ep.UpdateRepoState(commit.DID, models.RepoStateDesynced); err != nil {
		ep.Logger.Error("failed to update repo state to desynced", "did", commit.DID, "error", err)
		return err
	}

	return nil
}

func (ep *EventProcessor) ProcessIdentity(ctx context.Context, evt *comatproto.SyncSubscribeRepos_Identity) error {
	defer ep.lastSeq.Store(evt.Seq)
	return ep.RefreshIdentity(ctx, evt.Did)
}

// RefreshIdentity fetches the latest identity information for a DID.
func (ep *EventProcessor) RefreshIdentity(ctx context.Context, did string) error {
	ctx, span := tracer.Start(ctx, "RefreshIdentity")
	defer span.End()

	curr, err := ep.GetRepoState(did)
	if err != nil {
		return err
	} else if curr == nil {
		if ep.FullNetworkMode {
			if err := ep.EnsureRepo(did); err != nil {
				ep.Logger.Error("failed to auto-track repo", "did", did, "error", err)
				return err
			}
			return nil
		}
		return nil
	}

	if err := ep.Dir.Purge(ctx, syntax.DID(did).AtIdentifier()); err != nil {
		ep.Logger.Error("failed to purge identity cache", "did", did, "error", err)
	}

	id, err := ep.Dir.LookupDID(ctx, syntax.DID(did))
	if err != nil {
		return err
	}

	handleStr := id.Handle.String()
	if handleStr == curr.Handle {
		return nil
	}

	userEvt := &UserEvt{
		Did:      did,
		Handle:   handleStr,
		IsActive: curr.Status == models.AccountStatusActive,
		Status:   curr.Status,
	}
	if err := ep.Events.AddUserEvent(userEvt, func(tx *gorm.DB) error {
		return tx.Model(&models.Repo{}).
			Where("did = ?", did).
			Update("handle", handleStr).Error
	}); err != nil {
		ep.Logger.Error("failed to update handle", "did", did, "handle", handleStr, "error", err)
		return err
	}

	return nil
}

func (ep *EventProcessor) ProcessAccount(ctx context.Context, evt *comatproto.SyncSubscribeRepos_Account) error {
	defer ep.lastSeq.Store(evt.Seq)

	curr, err := ep.GetRepoState(evt.Did)
	if err != nil {
		return err
	} else if curr == nil {
		if ep.FullNetworkMode && evt.Active {
			if err := ep.EnsureRepo(evt.Did); err != nil {
				ep.Logger.Error("failed to auto-track repo", "did", evt.Did, "error", err)
				return err
			}
			return nil
		}
		return nil
	}

	var updateTo models.AccountStatus
	if evt.Active {
		updateTo = models.AccountStatusActive
	} else if evt.Status != nil && (*evt.Status == string(models.AccountStatusDeactivated) || *evt.Status == string(models.AccountStatusTakendown) || *evt.Status == string(models.AccountStatusSuspended) || *evt.Status == string(models.AccountStatusDeleted)) {
		updateTo = models.AccountStatus(*evt.Status)
	} else {
		// no-op for other events such as throttled or desynchronized
		return nil
	}

	if curr.Status == updateTo {
		return nil
	}

	userEvt := &UserEvt{
		Did:      curr.Did,
		Handle:   curr.Handle,
		IsActive: evt.Active,
		Status:   updateTo,
	}

	if updateTo == models.AccountStatusDeleted {
		if err := ep.Events.AddUserEvent(userEvt, func(tx *gorm.DB) error {
			return deleteRepo(tx, evt.Did)
		}); err != nil {
			ep.Logger.Error("failed to delete repo", "did", evt.Did, "error", err)
			return err
		}
	} else {
		if err := ep.Events.AddUserEvent(userEvt, func(tx *gorm.DB) error {
			return tx.Model(&models.Repo{}).
				Where("did = ?", evt.Did).
				Update("status", updateTo).Error
		}); err != nil {
			ep.Logger.Error("failed to update repo status", "did", evt.Did, "status", updateTo, "error", err)
			return err
		}
	}

	return nil
}

func (ep *EventProcessor) addToResyncBuffer(commit *Commit) error {
	jsonData, err := json.Marshal(commit)
	if err != nil {
		return err
	}
	return ep.DB.Create(&models.ResyncBuffer{
		Did:  commit.Did,
		Data: string(jsonData),
	}).Error
}

func (ep *EventProcessor) drainResyncBuffer(ctx context.Context, did string) error {
	var bufferedEvts []models.ResyncBuffer
	if err := ep.DB.Where("did = ?", did).Order("id ASC").Find(&bufferedEvts).Error; err != nil {
		return fmt.Errorf("failed to load buffered events: %w", err)
	}

	if len(bufferedEvts) == 0 {
		return nil
	}

	ep.Logger.Info("processing buffered resync events", "did", did, "count", len(bufferedEvts))

	for _, evt := range bufferedEvts {
		var commit Commit
		if err := json.Unmarshal([]byte(evt.Data), &commit); err != nil {
			return fmt.Errorf("failed to unmarshal buffered event: %w", err)
		}

		if err := ep.Events.AddCommit(&commit, func(tx *gorm.DB) error {
			return tx.Delete(&models.ResyncBuffer{}, "id = ?", evt.ID).Error
		}); err != nil {
			ep.Logger.Error("failed to process buffered commit", "did", commit.Did, "rev", commit.Rev, "error", err)
			return err
		}
	}

	ep.Logger.Info("processed buffered resync events", "did", did, "count", len(bufferedEvts))
	return nil
}

func (ep *EventProcessor) saveCursor(ctx context.Context) error {
	seq := ep.lastSeq.Load()
	if seq < 1 {
		return nil
	}

	return ep.DB.Save(&models.FirehoseCursor{
		Url:    ep.RelayUrl,
		Cursor: seq,
	}).Error
}

// RunCursorSaver periodically saves the firehose cursor to the database.
func (ep *EventProcessor) RunCursorSaver(ctx context.Context, interval time.Duration) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			if err := ep.saveCursor(ctx); err != nil {
				ep.Logger.Error("failed to save cursor on shutdown", "error", err, "relayUrl", ep.RelayUrl)
			}
			return
		case <-ticker.C:
			if err := ep.saveCursor(ctx); err != nil {
				ep.Logger.Error("failed to save cursor", "error", err, "relayUrl", ep.RelayUrl)
			}
		}
	}
}

func (ep *EventProcessor) ReadLastCursor(ctx context.Context, relayUrl string) (int64, error) {
	var cursor models.FirehoseCursor
	if err := ep.DB.Where("url = ?", relayUrl).First(&cursor).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			ep.Logger.Info("no pre-existing cursor in database", "relayUrl", relayUrl)
			return 0, nil
		}
		return 0, err
	}
	return cursor.Cursor, nil
}

func (ep *EventProcessor) GetRepoState(did string) (*models.Repo, error) {
	var r models.Repo
	if err := ep.DB.First(&r, "did = ?", did).Error; err != nil {
		if err != gorm.ErrRecordNotFound {
			return nil, err
		}
		return nil, nil
	}
	return &r, nil
}

func (ep *EventProcessor) UpdateRepoState(did string, state models.RepoState) error {
	return ep.DB.Model(&models.Repo{}).
		Where("did = ?", did).
		Update("state", state).Error
}

// EnsureRepo creates or updates a repository record in the database.
func (ep *EventProcessor) EnsureRepo(did string) error {
	return ep.DB.Save(&models.Repo{
		Did:    did,
		State:  models.RepoStatePending,
		Status: models.AccountStatusActive,
	}).Error
}
