package main

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"

	comatproto "github.com/bluesky-social/indigo/api/atproto"
	"github.com/bluesky-social/indigo/atproto/data"
	"github.com/bluesky-social/indigo/atproto/syntax"
	"github.com/bluesky-social/indigo/nexus/models"
	"github.com/bluesky-social/indigo/repo"
	"github.com/bluesky-social/indigo/xrpc"
	"github.com/ipfs/go-cid"
)

func (n *Nexus) backfillDid(ctx context.Context, did string) error {
	if err := n.db.Model(&models.Did{}).
		Where("did = ?", did).
		Updates(map[string]interface{}{
			"state":     models.RepoStateBackfilling,
			"rev":       "",
			"error_msg": "",
		}).Error; err != nil {
		return fmt.Errorf("failed to update state to backfilling: %w", err)
	}

	n.logger.Info("starting backfill", "did", did)

	rev, err := n.doBackfill(ctx, did)
	if err != nil {
		n.db.Model(&models.Did{}).
			Where("did = ?", did).
			Updates(map[string]interface{}{
				"state":     models.RepoStateError,
				"rev":       "",
				"error_msg": err.Error(),
			})
		return err
	}

	if err := n.db.Model(&models.Did{}).
		Where("did = ?", did).
		Updates(map[string]interface{}{
			"state":     models.RepoStateActive,
			"rev":       rev,
			"error_msg": "",
		}).Error; err != nil {
		return fmt.Errorf("failed to update state to active %w", err)
	}

	if err := n.EventProcessor.drainBackfillBuffer(ctx, did); err != nil {
		n.logger.Error("failed to drain backfill buffer events", "did", did, "error", err)
	}

	return nil
}

func (n *Nexus) doBackfill(ctx context.Context, did string) (string, error) {
	// Resolve DID to PDS
	ident, err := n.Dir.LookupDID(ctx, syntax.DID(did))
	if err != nil {
		return "", fmt.Errorf("failed to resolve DID: %w", err)
	}

	pdsURL := ident.PDSEndpoint()
	if pdsURL == "" {
		return "", fmt.Errorf("no PDS endpoint for DID: %s", did)
	}

	n.logger.Info("fetching repo from PDS", "did", did, "pds", pdsURL)

	// Create XRPC client
	client := &xrpc.Client{
		Client: &http.Client{},
		Host:   pdsURL,
	}

	// Call com.atproto.sync.getRepo
	repoBytes, err := comatproto.SyncGetRepo(ctx, client, did, "")
	if err != nil {
		return "", fmt.Errorf("failed to get repo: %w", err)
	}

	n.logger.Info("parsing repo CAR", "did", did, "size", len(repoBytes))

	// Parse the repo from CAR
	r, err := repo.ReadRepoFromCar(ctx, io.NopCloser(bytes.NewReader(repoBytes)))
	if err != nil {
		return "", fmt.Errorf("failed to read repo from CAR: %w", err)
	}

	rev := r.SignedCommit().Rev
	n.logger.Info("iterating repo records", "did", did, "rev", rev)

	// Pre-load existing CID mappings for this DID into memory
	var existingRecords []models.RepoRecord
	if err := n.db.Find(&existingRecords, "did = ?", did).Error; err != nil {
		return "", fmt.Errorf("failed to load existing records: %w", err)
	}

	// Build map: "collection/rkey" -> CID
	existingCids := make(map[string]string, len(existingRecords))
	for _, rec := range existingRecords {
		key := rec.Collection + "/" + rec.Rkey
		existingCids[key] = rec.Cid
	}
	n.logger.Info("pre-loaded existing records", "did", did, "count", len(existingCids))

	numRecords := 0

	err = r.ForEach(ctx, "", func(recPath string, recCid cid.Cid) error {
		collection, rkey, err := syntax.ParseRepoPath(recPath)
		if err != nil {
			n.logger.Error("invalid record path", "path", recPath, "error", err)
			return nil
		}

		collStr := collection.String()
		rkeyStr := rkey.String()
		cidStr := recCid.String()

		existingCid, exists := existingCids[recPath]
		action := "create"
		if exists {
			if existingCid == cidStr {
				return nil
			} else {
				action = "update"
			}
		}

		blk, err := r.Blockstore().Get(ctx, recCid)
		if err != nil {
			n.logger.Error("failed to get block", "path", recPath, "error", err)
			return nil
		}

		rec, err := data.UnmarshalCBOR(blk.RawData())
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
		return "", fmt.Errorf("failed to iterate repo: %w", err)
	}

	n.logger.Info("backfill repo complete", "did", did, "records", numRecords, "rev", rev)
	return rev, nil
}
