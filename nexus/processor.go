package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"sync/atomic"

	comatproto "github.com/bluesky-social/indigo/api/atproto"
	"github.com/bluesky-social/indigo/atproto/data"
	"github.com/bluesky-social/indigo/atproto/identity"
	"github.com/bluesky-social/indigo/atproto/repo"
	"github.com/bluesky-social/indigo/atproto/syntax"
	"github.com/bluesky-social/indigo/nexus/models"
	"gorm.io/gorm"
)

type Commit struct {
	Did string     `json:"did"`
	Rev string     `json:"rev"`
	Ops []CommitOp `json:"ops"`
}

type CommitOp struct {
	Collection string                 `json:"collection"`
	Rkey       string                 `json:"rkey"`
	Action     string                 `json:"action"`
	Record     map[string]interface{} `json:"record,omitempty"`
	Cid        string                 `json:"cid,omitempty"`
}

func (c *Commit) ToOps() []*Op {
	var ops []*Op
	for _, op := range c.Ops {
		ops = append(ops, &Op{
			Did:        c.Did,
			Rev:        c.Rev,
			Collection: op.Collection,
			Rkey:       op.Rkey,
			Action:     op.Action,
			Record:     op.Record,
			Cid:        op.Cid,
		})
	}
	return ops
}

type Op struct {
	Did        string                 `json:"did"`
	Rev        string                 `json:"rev"`
	Collection string                 `json:"collection"`
	Rkey       string                 `json:"rkey"`
	Action     string                 `json:"action"`
	Record     map[string]interface{} `json:"record,omitempty"`
	Cid        string                 `json:"cid,omitempty"`
}

type EventProcessor struct {
	Logger             *slog.Logger
	DB                 *gorm.DB
	Dir                identity.Directory
	PersistCursorEvery int
	RelayHost          string
	Outbox             *Outbox

	eventCount uint64
}

func (ep *EventProcessor) ProcessCommit(ctx context.Context, evt *comatproto.SyncSubscribeRepos_Commit) error {
	// @TODO this should happen at end of processing
	// Persist cursor periodically
	count := atomic.AddUint64(&ep.eventCount, 1)
	if count%uint64(ep.PersistCursorEvery) == 0 {
		if err := ep.persistCursor(ep.RelayHost, evt.Seq); err != nil {
			ep.Logger.Error("failed to persist cursor", "seq", evt.Seq, "error", err)
		}
	}

	var d models.Did
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

	commit, err := ep.validateCommit(ctx, evt, &d)
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

func (ep *EventProcessor) validateCommit(ctx context.Context, evt *comatproto.SyncSubscribeRepos_Commit, did *models.Did) (*Commit, error) {
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

			record, err := data.UnmarshalCBOR(recBytes)
			if err != nil {
				ep.Logger.Error("failed to unmarshal record", "did", evt.Repo, "path", op.Path, "error", err)
				continue
			}
			parsed.Record = record
		}

		parsedOps = append(parsedOps, parsed)
	}

	commit := &Commit{
		Did: evt.Repo,
		Ops: parsedOps,
	}

	return commit, nil
}

func (ep *EventProcessor) updateRepoState(commit *Commit) error {
	return ep.DB.Transaction(func(tx *gorm.DB) error {
		if err := tx.Model(&models.Did{}).
			Where("did = ?", commit.Did).
			Updates(map[string]interface{}{
				"rev": commit.Rev,
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
		var commit *Commit
		err := json.Unmarshal([]byte(evt.Data), commit)
		if err != nil {
			return fmt.Errorf("failed to unmarshal buffered event: %w", err)
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

		if err := ep.DB.Delete(&models.BackfillBuffer{}, "id = ?", evt.ID).Error; err != nil {
			ep.Logger.Error("failed to delete buffered event", "id", evt.ID, "did", commit.Did, "rev", commit.Rev, "error", err)
			return err
		}
	}

	ep.Logger.Info("processed buffered backfill events", "did", did, "count", len(bufferedEvts))
	return nil
}

func (ep *EventProcessor) persistCursor(relayHost string, seq int64) error {
	if seq <= 0 {
		return nil
	}

	cursor := models.Cursor{
		Host:   relayHost,
		Cursor: seq,
	}

	return ep.DB.Save(&cursor).Error
}
