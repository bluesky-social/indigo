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
	var did models.Repo
	err := n.db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Where("state IN (?)", []models.RepoState{models.RepoStatePending, models.RepoStateDesynced}).
			First(&did).Error; err != nil {
			return err
		}

		return tx.Model(&models.Repo{}).
			Where("did = ?", did.Did).
			Update("state", models.RepoStateResyncing).Error
	})

	if err != nil {
		if err == gorm.ErrRecordNotFound {
			return "", false, nil
		}
		return "", false, err
	}

	return did.Did, true, nil
}

func (n *Nexus) resyncDid(ctx context.Context, did string) error {
	n.logger.Info("starting resync", "did", did)

	err := n.doResync(ctx, did)
	if err != nil {
		n.db.Model(&models.Repo{}).
			Where("did = ?", did).
			Updates(map[string]interface{}{
				"state":     models.RepoStateError,
				"rev":       "",
				"prev_data": "",
				"error_msg": err.Error(),
			})
		return err
	}

	if err := n.EventProcessor.drainResyncBuffer(ctx, did); err != nil {
		n.logger.Error("failed to drain resync buffer events", "did", did, "error", err)
	}

	return nil
}

func (n *Nexus) doResync(ctx context.Context, did string) error {
	ident, err := n.Dir.LookupDID(ctx, syntax.DID(did))
	if err != nil {
		return fmt.Errorf("failed to resolve DID: %w", err)
	}

	pdsURL := ident.PDSEndpoint()
	if pdsURL == "" {
		return fmt.Errorf("no PDS endpoint for DID: %s", did)
	}

	n.logger.Info("fetching repo from PDS", "did", did, "pds", pdsURL)

	client := &xrpc.Client{
		Client: &http.Client{},
		Host:   pdsURL,
	}

	repoBytes, err := comatproto.SyncGetRepo(ctx, client, did, "")
	if err != nil {
		return fmt.Errorf("failed to get repo: %w", err)
	}

	n.logger.Info("parsing repo CAR", "did", did, "size", len(repoBytes))

	commit, r, err := repo.LoadRepoFromCAR(ctx, bytes.NewReader(repoBytes))
	if err != nil {
		return fmt.Errorf("failed to read repo from CAR: %w", err)
	}

	rev := commit.Rev
	n.logger.Info("iterating repo records", "did", did, "rev", rev)

	var existingRecords []models.RepoRecord
	if err := n.db.Find(&existingRecords, "did = ?", did).Error; err != nil {
		return fmt.Errorf("failed to load existing records: %w", err)
	}

	existingCids := make(map[string]string, len(existingRecords))
	for _, rec := range existingRecords {
		key := rec.Collection + "/" + rec.Rkey
		existingCids[key] = rec.Cid
	}
	n.logger.Info("pre-loaded existing records", "did", did, "count", len(existingCids))

	numRecords := 0

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

		op := &Op{
			Did:        did,
			Collection: collStr,
			Rkey:       rkeyStr,
			Action:     action,
			Record:     rec,
			Cid:        recCid.String(),
		}

		if err := n.outbox.Send(op); err != nil {
			return fmt.Errorf("failed to send op: %w", err)
		}

		repoRecord := models.RepoRecord{
			Did:        did,
			Collection: collStr,
			Rkey:       rkeyStr,
			Cid:        cidStr,
		}
		if err := n.db.Save(&repoRecord).Error; err != nil {
			n.logger.Error("failed to save repo record", "error", err, "did", did, "path", recPath)
		}

		numRecords++
		return nil
	})

	if err != nil {
		return fmt.Errorf("failed to iterate repo: %w", err)
	}

	if err := n.db.Model(&models.Repo{}).
		Where("did = ?", did).
		Updates(map[string]interface{}{
			"state":     models.RepoStateActive,
			"rev":       rev,
			"prev_data": commit.Data.String(),
			"error_msg": "",
		}).Error; err != nil {
		return fmt.Errorf("failed to update repo state to active %w", err)
	}

	n.logger.Info("resync repo complete", "did", did, "records", numRecords, "rev", rev)
	return nil
}

func (n *Nexus) resetPartiallyResynced() error {
	return n.db.Model(&models.Repo{}).
		Where("state = ?", models.RepoStateResyncing).
		Update("state", models.RepoStateDesynced).Error
}
