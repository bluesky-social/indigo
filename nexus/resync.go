package main

import (
	"bytes"
	"context"
	"fmt"
	"net/http"
	"time"

	comatproto "github.com/bluesky-social/indigo/api/atproto"
	"github.com/bluesky-social/indigo/atproto/atdata"
	"github.com/bluesky-social/indigo/atproto/repo"
	"github.com/bluesky-social/indigo/atproto/syntax"
	"github.com/bluesky-social/indigo/nexus/models"
	"github.com/bluesky-social/indigo/xrpc"
	"github.com/ipfs/go-cid"
	"gorm.io/gorm"
)

func (n *Nexus) runResyncWorker(ctx context.Context, workerID int) {
	logger := n.logger.With("worker", workerID)

	for {
		did, found, err := n.claimResyncJob(ctx)
		if err != nil {
			logger.Error("failed to claim resync job", "error", err)
			time.Sleep(3 * time.Second)
			continue
		}

		if !found {
			time.Sleep(3 * time.Second)
			continue
		}

		logger.Info("processing resync", "did", did)
		err = n.resyncDid(ctx, did)
		if err != nil {
			logger.Error("resync failed", "did", did, "error", err)
		}
	}
}

func (n *Nexus) claimResyncJob(ctx context.Context) (string, bool, error) {
	n.claimJobMu.Lock()
	defer n.claimJobMu.Unlock()

	var did string
	now := time.Now().Unix()
	result := n.db.Raw(`
		UPDATE repos
		SET state = ?
		WHERE did = (
			SELECT did FROM repos
			WHERE state IN (?, ?)
			AND retry_after <= ?
			ORDER BY retry_after ASC
			LIMIT 1
		)
		RETURNING did
		`, models.RepoStateResyncing, models.RepoStatePending, models.RepoStateDesynced, now).Scan(&did)
	if result.Error != nil {
		return "", false, result.Error
	}
	if result.RowsAffected == 0 {
		return "", false, nil
	}
	return did, true, nil
}

func (n *Nexus) resyncDid(ctx context.Context, did string) error {
	n.logger.Info("starting resync", "did", did)

	err := n.EventProcessor.RefreshIdentity(ctx, did)
	if err != nil {
		n.logger.Info("failed to refresh identity", "did", did, "error", err)
	}

	success, err := n.doResync(ctx, did)
	if !success {
		return n.handleResyncError(did, err)
	}

	if err := n.EventProcessor.drainResyncBuffer(ctx, did); err != nil {
		n.logger.Error("failed to drain resync buffer events", "did", did, "error", err)
	}

	return nil
}

const BATCH_SIZE = 100

func (n *Nexus) doResync(ctx context.Context, did string) (bool, error) {
	ident, err := n.Dir.LookupDID(ctx, syntax.DID(did))
	if err != nil {
		return false, fmt.Errorf("failed to resolve DID: %w", err)
	}

	pdsURL := ident.PDSEndpoint()
	if pdsURL == "" {
		return false, fmt.Errorf("no PDS endpoint for DID: %s", did)
	}

	n.pdsBackoffMu.RLock()
	backoffUntil, inBackoff := n.pdsBackoff[pdsURL]
	n.pdsBackoffMu.RUnlock()
	if inBackoff && time.Now().Before(backoffUntil) {
		return false, nil
	}

	n.logger.Info("fetching repo from PDS", "did", did, "pds", pdsURL)

	client := &xrpc.Client{
		Client: &http.Client{},
		Host:   pdsURL,
	}

	repoBytes, err := comatproto.SyncGetRepo(ctx, client, did, "")
	if err != nil {
		if isRateLimitError(err) {
			n.pdsBackoffMu.Lock()
			n.pdsBackoff[pdsURL] = time.Now().Add(10 * time.Second)
			n.pdsBackoffMu.Unlock()
			return false, nil
		}
		return false, fmt.Errorf("failed to get repo: %w", err)
	}

	n.logger.Info("parsing repo CAR", "did", did, "size", len(repoBytes))

	commit, r, err := repo.LoadRepoFromCAR(ctx, bytes.NewReader(repoBytes))
	if err != nil {
		return false, fmt.Errorf("failed to read repo from CAR: %w", err)
	}

	rev := commit.Rev
	n.logger.Info("iterating repo records", "did", did, "rev", rev)

	var existingRecords []models.RepoRecord
	if err := n.db.Find(&existingRecords, "did = ?", did).Error; err != nil {
		return false, fmt.Errorf("failed to load existing records: %w", err)
	}

	existingCids := make(map[string]string, len(existingRecords))
	for _, rec := range existingRecords {
		key := rec.Collection + "/" + rec.Rkey
		existingCids[key] = rec.Cid
	}
	n.logger.Info("pre-loaded existing records", "did", did, "count", len(existingCids))

	var evtBatch []*RecordEvt

	err = r.MST.Walk(func(recPathBytes []byte, recCid cid.Cid) error {
		recPath := string(recPathBytes)
		collection, rkey, err := syntax.ParseRepoPath(recPath)
		if err != nil {
			n.logger.Error("invalid record path", "path", recPath, "error", err)
			return nil
		}

		collStr := collection.String()
		rkeyStr := rkey.String()
		cidStr := recCid.String()

		// Filter collections - only process if matches filters
		if !matchesCollection(collStr, n.EventProcessor.CollectionFilters) {
			return nil
		}

		existingCid, exists := existingCids[recPath]
		if exists && existingCid == cidStr {
			return nil
		}

		action := "create"
		if exists {
			action = "update"
		}

		blk, err := r.RecordStore.Get(ctx, recCid)
		if err != nil {
			n.logger.Error("failed to get block", "path", recPath, "error", err)
			return nil
		}

		rec, err := atdata.UnmarshalCBOR(blk.RawData())
		if err != nil {
			n.logger.Error("failed to unmarshal record", "path", recPath, "error", err)
			return nil
		}

		evt := &RecordEvt{
			Live:       false,
			Did:        did,
			Collection: collStr,
			Rkey:       rkeyStr,
			Action:     action,
			Record:     rec,
			Cid:        recCid.String(),
		}
		evtBatch = append(evtBatch, evt)

		if len(evtBatch) >= BATCH_SIZE {
			if err := n.writeBatch(evtBatch); err != nil {
				n.logger.Error("failed to flush batch", "error", err, "did", did)
				return err
			}
			evtBatch = evtBatch[:0]
		}

		return nil
	})

	if err != nil {
		return false, fmt.Errorf("failed to iterate repo: %w", err)
	}

	if err := n.writeBatch(evtBatch); err != nil {
		return false, fmt.Errorf("failed to flush final batch: %w", err)
	}

	if err := n.db.Model(&models.Repo{}).
		Where("did = ?", did).
		Updates(map[string]interface{}{
			"state":       models.RepoStateActive,
			"rev":         rev,
			"prev_data":   commit.Data.String(),
			"error_msg":   "",
			"retry_count": 0,
			"retry_after": 0,
		}).Error; err != nil {
		return false, fmt.Errorf("failed to update repo state to active %w", err)
	}

	n.logger.Info("resync repo complete", "did", did, "rev", rev)
	return true, nil
}

func (n *Nexus) writeBatch(evtBatch []*RecordEvt) error {
	if len(evtBatch) == 0 {
		return nil
	}

	recordBatch := make([]*models.RepoRecord, 0, len(evtBatch))
	for _, evt := range evtBatch {
		recordBatch = append(recordBatch, &models.RepoRecord{
			Did:        evt.Did,
			Collection: evt.Collection,
			Rkey:       evt.Rkey,
			Cid:        evt.Cid,
		})
	}

	if err := n.db.Transaction(func(tx *gorm.DB) error {
		for _, record := range recordBatch {
			if err := tx.Save(&record).Error; err != nil {
				return err
			}
		}

		for _, evt := range evtBatch {
			if err := persistRecordEvt(tx, evt); err != nil {
				return err
			}
		}

		return nil
	}); err != nil {
		return err
	}

	n.outbox.Notify()
	return nil
}

func (n *Nexus) handleResyncError(did string, err error) error {
	var state models.RepoState
	var errMsg string
	if err == nil {
		state = models.RepoStateDesynced
		errMsg = ""
	} else {
		state = models.RepoStateError
		errMsg = err.Error()
	}

	repo, err := n.EventProcessor.GetRepoState(did)
	if err != nil {
		return err
	}

	// start a 1 min & go up to 1 hr between retries
	retryAfter := time.Now().Add(60 * backoff(repo.RetryCount, 60))

	dbErr := n.db.Model(&models.Repo{}).
		Where("did = ?", did).
		Updates(map[string]interface{}{
			"state":       state,
			"error_msg":   errMsg,
			"retry_count": repo.RetryCount + 1,
			"retry_after": retryAfter.Unix(),
		}).Error
	if dbErr != nil {
		return dbErr
	} else {
		return err
	}

}

func (n *Nexus) resetPartiallyResynced() error {
	return n.db.Model(&models.Repo{}).
		Where("state = ?", models.RepoStateResyncing).
		Update("state", models.RepoStateDesynced).Error
}
