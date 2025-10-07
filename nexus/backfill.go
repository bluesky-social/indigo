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

func (n *Nexus) backfillDid(ctx context.Context, did string) {
	// Mark as backfilling
	if err := n.UpdateRepoState(did, models.RepoStateBackfilling, "", ""); err != nil {
		n.logger.Error("failed to update state to backfilling", "error", err, "did", did)
		return
	}

	n.logger.Info("starting backfill", "did", did)

	// Perform backfill
	rev, err := n.backfillRepo(ctx, did)
	if err != nil {
		n.logger.Error("backfill failed", "error", err, "did", did)
		// Mark as error state
		if updateErr := n.UpdateRepoState(did, models.RepoStateError, "", err.Error()); updateErr != nil {
			n.logger.Error("failed to update state to error", "error", updateErr, "did", did)
		}
		// Clean up buffered events
		n.mu.Lock()
		delete(n.backfillBuffer, did)
		n.mu.Unlock()
		return
	}

	// Drain buffered events that came in during backfill
	if err := n.drainBackfillBuffer(ctx, did); err != nil {
		n.logger.Error("failed to drain backfill buffer", "error", err, "did", did)
		if updateErr := n.UpdateRepoState(did, models.RepoStateError, rev, err.Error()); updateErr != nil {
			n.logger.Error("failed to update state to error", "error", updateErr, "did", did)
		}
		return
	}

	// Mark as active
	if err := n.UpdateRepoState(did, models.RepoStateActive, rev, ""); err != nil {
		n.logger.Error("failed to update state to active", "error", err, "did", did)
		return
	}

	n.logger.Info("backfill complete", "did", did, "rev", rev)
}

func (n *Nexus) drainBackfillBuffer(ctx context.Context, did string) error {
	// Get buffered events from memory
	n.mu.Lock()
	bufferedOps := n.backfillBuffer[did]
	delete(n.backfillBuffer, did)
	n.mu.Unlock()

	if len(bufferedOps) == 0 {
		return nil
	}

	n.logger.Info("draining backfill buffer from memory", "did", did, "count", len(bufferedOps))

	// Send each buffered event
	for _, op := range bufferedOps {
		if err := n.outbox.Send(op); err != nil {
			return err
		}
	}

	n.logger.Info("drained backfill buffer", "did", did, "count", len(bufferedOps))
	return nil
}

func (n *Nexus) backfillRepo(ctx context.Context, did string) (string, error) {
	// Resolve DID to PDS
	ident, err := n.Dir.LookupDID(ctx, syntax.DID(did))
	if err != nil {
		return "", fmt.Errorf("failed to resolve DID: %w", err)
	}

	pdsHost := ident.PDSEndpoint()
	if pdsHost == "" {
		return "", fmt.Errorf("no PDS endpoint for DID: %s", did)
	}

	n.logger.Info("fetching repo from PDS", "did", did, "pds", pdsHost)

	// Create XRPC client
	client := &xrpc.Client{
		Client: &http.Client{},
		Host:   pdsHost,
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

	numRecords := 0

	// Iterate through all records
	err = r.ForEach(ctx, "", func(recordPath string, nodeCid cid.Cid) error {
		// Get the record bytes
		blk, err := r.Blockstore().Get(ctx, nodeCid)
		if err != nil {
			n.logger.Error("failed to get block", "path", recordPath, "error", err)
			return nil // Skip this record
		}

		raw := blk.RawData()

		// Unmarshal to get the actual record
		rec, err := data.UnmarshalCBOR(raw)
		if err != nil {
			n.logger.Error("failed to unmarshal record", "path", recordPath, "error", err)
			return nil
		}

		// Parse the path to get collection and rkey
		collection, rkey, err := syntax.ParseRepoPath(recordPath)
		if err != nil {
			n.logger.Error("invalid record path", "path", recordPath, "error", err)
			return nil
		}

		// Send as create event
		op := &Op{
			Did:        did,
			Collection: collection.String(),
			Rkey:       rkey.String(),
			Action:     "create",
			Record:     rec,
			Cid:        nodeCid.String(),
		}

		if err := n.outbox.Send(op); err != nil {
			return fmt.Errorf("failed to send op: %w", err)
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
