package main

import (
	"context"
	"fmt"
	"log/slog"
	"sync/atomic"
	"time"

	comatproto "github.com/bluesky-social/indigo/api/atproto"
	"github.com/bluesky-social/indigo/atproto/atdata"
	"github.com/bluesky-social/indigo/atproto/repo"
	"github.com/bluesky-social/indigo/atproto/syntax"
	"github.com/bluesky-social/indigo/cmd/nexus/models"
	"go.opentelemetry.io/otel"
	"gorm.io/gorm"
)

var tracer = otel.Tracer("nexus")

type FirehoseProcessor struct {
	Logger       *slog.Logger
	DB           *gorm.DB
	RelayUrl     string
	Events       *EventManager
	repos        *RepoManager
	resyncBuffer *ResyncBuffer

	FullNetworkMode   bool
	SignalCollection  string
	CollectionFilters []string

	lastSeq atomic.Int64
}

// ProcessCommit validates and applies a commit event from the firehose.
func (fp *FirehoseProcessor) ProcessCommit(ctx context.Context, evt *comatproto.SyncSubscribeRepos_Commit) error {
	ctx, span := tracer.Start(ctx, "ProcessCommit")
	defer span.End()

	defer fp.lastSeq.Store(evt.Seq)

	curr, err := fp.repos.GetRepoState(evt.Repo)
	if err != nil {
		return err
	} else if curr == nil {
		shouldTrack := fp.FullNetworkMode || (fp.SignalCollection != "" && evtHasSignalCollection(evt, fp.SignalCollection))
		if shouldTrack {
			if err := fp.repos.EnsureRepo(evt.Repo); err != nil {
				fp.Logger.Error("failed to auto-track repo", "did", evt.Repo, "error", err)
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
		fp.Logger.Debug("skipping replayed event", "did", evt.Repo, "eventRev", evt.Rev, "currentRev", curr.Rev)
		return nil
	}

	if evt.PrevData == nil {
		fp.Logger.Debug("legacy commit event, skipping prev data check", "did", evt.Repo, "rev", evt.Rev)
	} else if evt.PrevData.String() != curr.PrevData {
		fp.Logger.Warn("repo state desynced", "did", evt.Repo, "rev", evt.Rev)
		// gets picked up by resync workers
		if err := fp.UpdateRepoState(evt.Repo, models.RepoStateDesynced); err != nil {
			fp.Logger.Error("failed to update repo state to desynced", "did", evt.Repo, "error", err)
			return err
		}
		return nil
	}

	commit, err := fp.validateCommit(ctx, evt)
	if err != nil {
		fp.Logger.Error("failed to parse operations", "did", evt.Repo, "error", err)
		return err
	}

	// filter ops to only matching collections after validation (since all ops are necessary for commit validation)
	filteredOps := []CommitOp{}
	for _, op := range commit.Ops {
		if matchesCollection(op.Collection, fp.CollectionFilters) {
			filteredOps = append(filteredOps, op)
		}
	}
	if len(filteredOps) == 0 {
		return nil
	}
	commit.Ops = filteredOps

	if curr.State == models.RepoStateResyncing {
		if err := fp.resyncBuffer.add(commit); err != nil {
			fp.Logger.Error("failed to buffer commit", "did", evt.Repo, "error", err)
			return err
		}
	}

	if err := fp.Events.AddCommit(commit, func(tx *gorm.DB) error {
		return nil
	}); err != nil {
		fp.Logger.Error("failed to update repo state", "did", commit.Did, "rev", commit.Rev, "error", err)
		return err
	}

	return nil
}

func (fp *FirehoseProcessor) validateCommit(ctx context.Context, evt *comatproto.SyncSubscribeRepos_Commit) (*Commit, error) {
	if err := repo.VerifyCommitSignature(ctx, fp.repos.IdDir, evt); err != nil {
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
				fp.Logger.Error("failed to get record bytes", "did", evt.Repo, "path", op.Path, "error", err)
				continue
			}

			record, err := atdata.UnmarshalCBOR(recBytes)
			if err != nil {
				// do not skip here
				// we end up storing the CID but not passing the record along in the outbox
				fp.Logger.Error("failed to unmarshal record", "did", evt.Repo, "path", op.Path, "error", err)
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
func (fp *FirehoseProcessor) ProcessSync(ctx context.Context, evt *comatproto.SyncSubscribeRepos_Sync) error {
	ctx, span := tracer.Start(ctx, "ProcessSync")
	defer span.End()

	defer fp.lastSeq.Store(evt.Seq)

	curr, err := fp.repos.GetRepoState(evt.Did)
	if err != nil {
		return err
	} else if curr == nil {
		if fp.FullNetworkMode {
			if err := fp.repos.EnsureRepo(evt.Did); err != nil {
				fp.Logger.Error("failed to auto-track repo", "did", evt.Did, "error", err)
				return err
			}
			return nil
		}
		return nil
	}

	commit, err := repo.VerifySyncMessage(ctx, fp.repos.IdDir, evt)
	if err != nil {
		return fmt.Errorf("failed to verify sync message: %w", err)
	}

	if curr.State != models.RepoStateActive {
		return nil
	}

	if curr.Rev != "" && commit.Rev <= curr.Rev {
		fp.Logger.Debug("skipping replayed event", "did", commit.DID, "eventRev", commit.Rev, "currentRev", curr.Rev)
		return nil
	}

	if curr.PrevData == commit.Data.String() {
		fp.Logger.Debug("skipping noop sync event", "did", commit.DID, "rev", commit.Rev)
		return nil
	}

	if err := fp.UpdateRepoState(commit.DID, models.RepoStateDesynced); err != nil {
		fp.Logger.Error("failed to update repo state to desynced", "did", commit.DID, "error", err)
		return err
	}

	return nil
}

func (fp *FirehoseProcessor) ProcessIdentity(ctx context.Context, evt *comatproto.SyncSubscribeRepos_Identity) error {
	defer fp.lastSeq.Store(evt.Seq)

	curr, err := fp.repos.GetRepoState(evt.Did)
	if err != nil {
		return err
	} else if curr == nil {
		if fp.FullNetworkMode {
			if err := fp.repos.EnsureRepo(evt.Did); err != nil {
				return err
			}
			return nil
		}
		return nil
	}

	return fp.repos.RefreshIdentity(ctx, evt.Did)
}

func (fp *FirehoseProcessor) ProcessAccount(ctx context.Context, evt *comatproto.SyncSubscribeRepos_Account) error {
	defer fp.lastSeq.Store(evt.Seq)

	curr, err := fp.repos.GetRepoState(evt.Did)
	if err != nil {
		return err
	} else if curr == nil {
		if fp.FullNetworkMode && evt.Active {
			if err := fp.repos.EnsureRepo(evt.Did); err != nil {
				fp.Logger.Error("failed to auto-track repo", "did", evt.Did, "error", err)
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
		if err := fp.Events.AddUserEvent(userEvt, func(tx *gorm.DB) error {
			return deleteRepo(tx, evt.Did)
		}); err != nil {
			fp.Logger.Error("failed to delete repo", "did", evt.Did, "error", err)
			return err
		}
	} else {
		if err := fp.Events.AddUserEvent(userEvt, func(tx *gorm.DB) error {
			return tx.Model(&models.Repo{}).
				Where("did = ?", evt.Did).
				Update("status", updateTo).Error
		}); err != nil {
			fp.Logger.Error("failed to update repo status", "did", evt.Did, "status", updateTo, "error", err)
			return err
		}
	}

	return nil
}

func (fp *FirehoseProcessor) saveCursor(ctx context.Context) error {
	seq := fp.lastSeq.Load()
	if seq < 1 {
		return nil
	}

	return fp.DB.Save(&models.FirehoseCursor{
		Url:    fp.RelayUrl,
		Cursor: seq,
	}).Error
}

// RunCursorSaver periodically saves the firehose cursor to the database.
func (fp *FirehoseProcessor) RunCursorSaver(ctx context.Context, interval time.Duration) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			if err := fp.saveCursor(ctx); err != nil {
				fp.Logger.Error("failed to save cursor on shutdown", "error", err, "relayUrl", fp.RelayUrl)
			}
			return
		case <-ticker.C:
			if err := fp.saveCursor(ctx); err != nil {
				fp.Logger.Error("failed to save cursor", "error", err, "relayUrl", fp.RelayUrl)
			}
		}
	}
}

func (fp *FirehoseProcessor) ReadLastCursor(ctx context.Context, relayUrl string) (int64, error) {
	var cursor models.FirehoseCursor
	if err := fp.DB.Where("url = ?", relayUrl).First(&cursor).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			fp.Logger.Info("no pre-existing cursor in database", "relayUrl", relayUrl)
			return 0, nil
		}
		return 0, err
	}
	return cursor.Cursor, nil
}

func (fp *FirehoseProcessor) UpdateRepoState(did string, state models.RepoState) error {
	return fp.DB.Model(&models.Repo{}).
		Where("did = ?", did).
		Update("state", state).Error
}
