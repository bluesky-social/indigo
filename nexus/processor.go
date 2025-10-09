package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"sync"
	"time"

	comatproto "github.com/bluesky-social/indigo/api/atproto"
	"github.com/bluesky-social/indigo/atproto/atdata"
	"github.com/bluesky-social/indigo/atproto/identity"
	"github.com/bluesky-social/indigo/atproto/repo"
	"github.com/bluesky-social/indigo/atproto/syntax"
	"github.com/bluesky-social/indigo/nexus/models"
	"gorm.io/gorm"
)

type EventProcessor struct {
	Logger    *slog.Logger
	DB        *gorm.DB
	Dir       identity.Directory
	RelayHost string
	Outbox    *Outbox

	lastSeq int64
	seqMu   sync.Mutex
}

func (ep *EventProcessor) ProcessCommit(ctx context.Context, evt *comatproto.SyncSubscribeRepos_Commit) error {
	defer ep.trackLastSeq(evt.Seq)

	var d models.Repo
	if err := ep.DB.First(&d, "did = ?", evt.Repo).Error; err != nil {
		if err != gorm.ErrRecordNotFound {
			ep.Logger.Error("failed to get repo state", "did", evt.Repo, "error", err)
		}
		return nil
	}

	if d.State == models.RepoStatePending {
		return nil
	}

	if d.Rev != "" && evt.Rev <= d.Rev {
		ep.Logger.Debug("skipping replayed event", "did", evt.Repo, "eventRev", evt.Rev, "currentRev", d.Rev)
		return nil
	}

	if evt.PrevData == nil {
		ep.Logger.Debug("legacy commit event, skipping prev data check", "did", evt.Repo, "rev", evt.Rev)
	} else if evt.PrevData.String() != d.PrevData {
		// @TODO DESYNCED
		ep.Logger.Warn("repo state desynced", "did", evt.Repo, "rev", evt.Rev)
	}

	commit, err := ep.validateCommit(ctx, evt)
	if err != nil {
		ep.Logger.Error("failed to parse operations", "did", evt.Repo, "error", err)
		return err
	}

	if d.State == models.RepoStateBackfilling {
		if err := ep.addToBackfillBuffer(commit); err != nil {
			ep.Logger.Error("failed to buffer commit", "did", evt.Repo, "error", err)
			return err
		}
	}

	for _, op := range commit.ToOps() {
		if err := ep.Outbox.Send(op); err != nil {
			ep.Logger.Error("failed to send to outbox", "did", commit.Did, "rev", commit.Rev, "error", err)
			return err
		}
	}

	if err := ep.updateRepoState(commit); err != nil {
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
				ep.Logger.Error("failed to unmarshal record", "did", evt.Repo, "path", op.Path, "error", err)
				continue
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

func (ep *EventProcessor) updateRepoState(commit *Commit) error {
	return ep.DB.Transaction(func(tx *gorm.DB) error {
		if err := tx.Model(&models.Repo{}).
			Where("did = ?", commit.Did).
			Updates(map[string]interface{}{
				"rev":       commit.Rev,
				"prev_data": commit.DataCid,
			}).Error; err != nil {
			return err
		}

		for _, op := range commit.Ops {
			if op.Action == "delete" {
				if err := tx.Delete(&models.RepoRecord{}, "did = ? AND collection = ? AND rkey = ?", commit.Did, op.Collection, op.Rkey).Error; err != nil {
					return err
				}
			} else {
				repoRecord := models.RepoRecord{
					Did:        commit.Did,
					Collection: op.Collection,
					Rkey:       op.Rkey,
					Cid:        op.Cid,
				}
				if err := tx.Save(&repoRecord).Error; err != nil {
					return err
				}
			}
		}

		return nil
	})
}

func (ep *EventProcessor) addToBackfillBuffer(commit *Commit) error {
	jsonData, err := json.Marshal(commit)
	if err != nil {
		return err
	}
	return ep.DB.Create(&models.BackfillBuffer{
		Did:  commit.Did,
		Data: string(jsonData),
	}).Error
}

func (ep *EventProcessor) drainBackfillBuffer(ctx context.Context, did string) error {
	var bufferedEvts []models.BackfillBuffer
	if err := ep.DB.Where("did = ?", did).Order("id ASC").Find(&bufferedEvts).Error; err != nil {
		return fmt.Errorf("failed to load buffered events: %w", err)
	}

	if len(bufferedEvts) == 0 {
		return nil
	}

	ep.Logger.Info("processing buffered backfill events", "did", did, "count", len(bufferedEvts))

	for _, evt := range bufferedEvts {
		var commit Commit
		if err := json.Unmarshal([]byte(evt.Data), &commit); err != nil {
			return fmt.Errorf("failed to unmarshal buffered event: %w", err)
		}

		for _, op := range commit.ToOps() {
			if err := ep.Outbox.Send(op); err != nil {
				ep.Logger.Error("failed to send to outbox", "did", commit.Did, "rev", commit.Rev, "error", err)
				return err
			}
		}

		if err := ep.updateRepoState(&commit); err != nil {
			ep.Logger.Error("failed to update repo state", "did", commit.Did, "rev", commit.Rev, "error", err)
			return err
		}

		if err := ep.DB.Delete(&models.BackfillBuffer{}, "id = ?", evt.ID).Error; err != nil {
			ep.Logger.Error("failed to delete buffered event", "id", evt.ID, "did", commit.Did, "rev", commit.Rev, "error", err)
			return err
		}
	}

	ep.Logger.Info("processed buffered backfill events", "did", did, "count", len(bufferedEvts))
	return nil
}

func (ep *EventProcessor) trackLastSeq(seq int64) {
	ep.seqMu.Lock()
	ep.lastSeq = seq
	ep.seqMu.Unlock()
}

func (ep *EventProcessor) saveCursor(ctx context.Context) error {
	ep.seqMu.Lock()
	seq := ep.lastSeq
	ep.seqMu.Unlock()

	if seq == 0 {
		return nil
	}

	return ep.DB.Save(&models.Cursor{
		Host:   ep.RelayHost,
		Cursor: seq,
	}).Error
}

func (ep *EventProcessor) RunCursorSaver(ctx context.Context, interval time.Duration) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			if err := ep.saveCursor(ctx); err != nil {
				ep.Logger.Error("failed to save cursor on shutdown", "error", err)
			}
			return
		case <-ticker.C:
			if err := ep.saveCursor(ctx); err != nil {
				ep.Logger.Error("failed to save cursor", "error", err)
			}
		}
	}
}

func (ep *EventProcessor) ReadLastCursor(ctx context.Context, relayHost string) (int64, error) {
	var cursor models.Cursor
	if err := ep.DB.Where("host = ?", relayHost).First(&cursor).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			ep.Logger.Info("no pre-existing cursor in database", "relayHost", relayHost)
			return 0, nil
		}
		return 0, err
	}
	return cursor.Cursor, nil
}
