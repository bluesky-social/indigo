package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"sync"
	"time"

	comatproto "github.com/bluesky-social/indigo/api/atproto"
	"github.com/bluesky-social/indigo/atproto/atclient"
	"github.com/bluesky-social/indigo/atproto/atdata"
	repolib "github.com/bluesky-social/indigo/atproto/repo"
	"github.com/bluesky-social/indigo/atproto/syntax"
	"github.com/bluesky-social/indigo/cmd/tap/models"
	"github.com/ipfs/go-cid"
	"go.opentelemetry.io/otel/attribute"
	"gorm.io/gorm"
)

type Resyncer struct {
	logger *slog.Logger
	db     *gorm.DB

	events *EventManager
	repos  *RepoManager

	claimJobMu sync.Mutex

	repoFetchTimeout  time.Duration
	collectionFilters []string
	parallelism       int

	pdsBackoff   map[string]time.Time
	pdsBackoffMu sync.RWMutex
}

func NewResyncer(logger *slog.Logger, db *gorm.DB, repos *RepoManager, events *EventManager, config *TapConfig) *Resyncer {
	return &Resyncer{
		logger:            logger.With("component", "resyncer"),
		db:                db,
		events:            events,
		repos:             repos,
		repoFetchTimeout:  config.RepoFetchTimeout,
		collectionFilters: config.CollectionFilters,
		parallelism:       config.ResyncParallelism,
		pdsBackoff:        make(map[string]time.Time),
	}
}

func (r *Resyncer) run(ctx context.Context) {
	for i := 0; i < r.parallelism; i++ {
		go r.runResyncWorker(ctx, i)
	}
}

func (r *Resyncer) runResyncWorker(ctx context.Context, workerID int) {
	logger := r.logger.With("worker", workerID)

	for {
		r.events.WaitForReady(ctx)

		// Before calling claimResyncJob, check for context cancellation.
		// If the context is cancelled after this check, claimResyncJob will return an
		// error then sleep briefly before coming back to check the context again.
		// Checking here avoids tring to enumerate and classify all the errors that
		// claimResyncJob might return due to shutdown.
		select {
		case <-ctx.Done():
			logger.Info("resync worker shutting down", "error", ctx.Err())
			return
		default:
			did, found, err := r.claimResyncJob(ctx)
			if err != nil {
				logger.Error("failed to claim resync job", "error", err)
				time.Sleep(time.Second)
				continue
			}

			if !found {
				time.Sleep(time.Second)
				continue
			}

			logger.Info("processing resync", "did", did)
			err = r.resyncDid(ctx, did)
			if err != nil {
				logger.Error("resync failed", "did", did, "error", err)
			}
		}
	}
}

func (r *Resyncer) claimResyncJob(ctx context.Context) (string, bool, error) {
	r.claimJobMu.Lock()
	defer r.claimJobMu.Unlock()

	var did string
	now := time.Now().Unix()
	result := r.db.WithContext(ctx).Raw(`
		UPDATE repos
		SET state = ?
		WHERE did = (
			SELECT did FROM repos
			WHERE state IN (?, ?, ?)
			AND (retry_after = 0 OR retry_after < ?)
			LIMIT 1
		)
		RETURNING did
		`, models.RepoStateResyncing, models.RepoStatePending, models.RepoStateDesynchronized, models.RepoStateError, now).Scan(&did)
	if result.Error != nil {
		return "", false, result.Error
	}
	if result.RowsAffected == 0 {
		return "", false, nil
	}
	return did, true, nil
}

func (r *Resyncer) resyncDid(ctx context.Context, did string) error {
	ctx, span := tracer.Start(ctx, "resyncDid")
	span.SetAttributes(attribute.String("did", did))
	defer span.End()

	resyncsStarted.Inc()
	startTime := time.Now()

	r.logger.Info("starting resync", "did", did)

	err := r.repos.RefreshIdentity(ctx, did)
	if err != nil {
		r.logger.Info("failed to refresh identity", "did", did, "error", err)
	}

	success, err := r.doResync(ctx, did)
	if !success {
		resyncsFailed.Inc()
		resyncDuration.Observe(time.Since(startTime).Seconds())
		return r.handleResyncError(ctx, did, err)
	}

	resyncsCompleted.Inc()
	resyncDuration.Observe(time.Since(startTime).Seconds())

	if err := r.drainResyncBuffer(ctx, did); err != nil {
		r.logger.Error("failed to drain resync buffer events", "did", did, "error", err)
	}

	return nil
}

const batchSize = 100

func (r *Resyncer) doResync(ctx context.Context, did string) (bool, error) {
	ctx, span := tracer.Start(ctx, "doResync")
	span.SetAttributes(attribute.String("did", did))
	defer span.End()

	ident, err := r.repos.idDir.LookupDID(ctx, syntax.DID(did))
	if err != nil {
		return false, fmt.Errorf("failed to resolve DID: %w", err)
	}

	pdsURL := ident.PDSEndpoint()
	if pdsURL == "" {
		return false, fmt.Errorf("no PDS endpoint for DID: %s", did)
	}

	signingKey, err := ident.PublicKey()
	if err != nil {
		return false, fmt.Errorf("failed to get public key: %w", err)
	}

	r.pdsBackoffMu.RLock()
	backoffUntil, inBackoff := r.pdsBackoff[pdsURL]
	r.pdsBackoffMu.RUnlock()
	if inBackoff && time.Now().Before(backoffUntil) {
		return false, nil
	}

	r.logger.Info("fetching repo from PDS", "did", did, "pds", pdsURL)

	client := atclient.NewAPIClient(pdsURL)
	client.Headers.Set("User-Agent", userAgent())
	timeout := r.repoFetchTimeout
	if timeout <= 0 {
		timeout = 30 * time.Second
	}
	client.Client = &http.Client{
		Timeout: timeout,
	}

	repoBytes, err := comatproto.SyncGetRepo(ctx, client, did, "")
	if err != nil {
		if isRateLimitError(err) {
			r.pdsBackoffMu.Lock()
			r.pdsBackoff[pdsURL] = time.Now().Add(10 * time.Second)
			r.pdsBackoffMu.Unlock()
			return false, nil
		}
		return false, fmt.Errorf("failed to get repo: %w", err)
	}

	r.logger.Info("parsing repo CAR", "did", did, "size", len(repoBytes))

	commit, repo, err := repolib.LoadRepoFromCAR(ctx, bytes.NewReader(repoBytes))
	if err != nil {
		return false, fmt.Errorf("failed to read repo from CAR: %w", err)
	}

	err = commit.VerifySignature(signingKey)
	if err != nil {
		return false, fmt.Errorf("failed to verify signature: %w", err)
	}

	rev := commit.Rev
	r.logger.Info("iterating repo records", "did", did, "rev", rev)

	var existingRecords []models.RepoRecord
	if err := r.db.WithContext(ctx).Find(&existingRecords, "did = ?", did).Error; err != nil {
		return false, fmt.Errorf("failed to load existing records: %w", err)
	}

	existingCids := make(map[string]string, len(existingRecords))
	for _, rec := range existingRecords {
		key := rec.Collection + "/" + rec.Rkey
		existingCids[key] = rec.Cid
	}
	r.logger.Info("pre-loaded existing records", "did", did, "count", len(existingCids))

	evtBatch := make([]*RecordEvt, 0, batchSize)

	err = repo.MST.Walk(func(recPathBytes []byte, recCid cid.Cid) error {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		recPath := string(recPathBytes)
		collection, rkey, err := syntax.ParseRepoPath(recPath)
		if err != nil {
			r.logger.Error("invalid record path", "path", recPath, "error", err)
			return nil
		}

		collStr := collection.String()
		rkeyStr := rkey.String()
		cidStr := recCid.String()

		// Filter collections - only process if matches filters
		if !matchesCollection(collStr, r.collectionFilters) {
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

		blk, err := repo.RecordStore.Get(ctx, recCid)
		if err != nil {
			r.logger.Error("failed to get block", "path", recPath, "error", err)
			return nil
		}

		rec, err := atdata.UnmarshalCBOR(blk.RawData())
		if err != nil {
			// do not fail here
			// we end up storing the CID but not passing the record along in the outbox
			r.logger.Error("failed to unmarshal record", "did", did, "path", recPath, "error", err)
		}

		evt := &RecordEvt{
			Live:       false,
			Did:        did,
			Rev:        rev,
			Collection: collStr,
			Rkey:       rkeyStr,
			Action:     action,
			Record:     rec,
			Cid:        recCid.String(),
		}
		evtBatch = append(evtBatch, evt)

		if len(evtBatch) >= batchSize {
			r.events.WaitForReady(ctx)
			if err := r.events.AddRecordEvents(ctx, evtBatch, false, func(tx *gorm.DB) error {
				return nil
			}); err != nil {
				r.logger.Error("failed to flush batch", "error", err, "did", did)
				return err
			}
			evtBatch = evtBatch[:0]
		}

		return nil
	})

	if err != nil {
		return false, fmt.Errorf("failed to iterate repo: %w", err)
	}

	if err := r.events.AddRecordEvents(ctx, evtBatch, false, func(tx *gorm.DB) error {
		return nil
	}); err != nil {
		return false, fmt.Errorf("failed to flush final batch: %w", err)
	}

	if err := r.db.WithContext(ctx).Model(&models.Repo{}).
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

	r.logger.Info("resync repo complete", "did", did, "rev", rev)
	return true, nil
}

func (r *Resyncer) handleResyncError(ctx context.Context, did string, err error) error {
	var state models.RepoState
	var errMsg string
	if err == nil {
		state = models.RepoStateDesynchronized
		errMsg = ""
	} else {
		state = models.RepoStateError
		errMsg = err.Error()
	}

	repo, err := r.repos.GetRepoState(ctx, did)
	if err != nil {
		return err
	}

	// start a 1 min & go up to 1 hr between retries
	retryAfter := time.Now().Add(backoff(repo.RetryCount, 60) * 60)

	dbErr := r.db.WithContext(ctx).Model(&models.Repo{}).
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

func (r *Resyncer) resetPartiallyResynced(ctx context.Context) error {
	return r.db.WithContext(ctx).Model(&models.Repo{}).
		Where("state = ?", models.RepoStateResyncing).
		Update("state", models.RepoStateDesynchronized).Error
}

func (r *Resyncer) drainResyncBuffer(ctx context.Context, did string) error {
	var bufferedEvts []models.ResyncBuffer
	if err := r.events.db.WithContext(ctx).Where("did = ?", did).Order("id ASC").Find(&bufferedEvts).Error; err != nil {
		return fmt.Errorf("failed to load buffered events: %w", err)
	}

	if len(bufferedEvts) == 0 {
		return nil
	}

	curr, err := r.repos.GetRepoState(ctx, did)
	if err != nil {
		return fmt.Errorf("failed to get repo state: %w", err)
	}

	for _, evt := range bufferedEvts {
		var commit Commit
		if err := json.Unmarshal([]byte(evt.Data), &commit); err != nil {
			return fmt.Errorf("failed to unmarshal buffered event: %w", err)
		}

		// if this commit doesn't stack neatly on current state of tracked repo then we skip
		// NOTE: the check against the empty string can be eliminated after we start refusing legacy commit events
		if commit.PrevData != "" && commit.PrevData != curr.PrevData {
			continue
		}

		if err := r.events.AddCommit(ctx, &commit, func(tx *gorm.DB) error {
			return tx.Delete(&models.ResyncBuffer{}, "id = ?", evt.ID).Error
		}); err != nil {
			return err
		}
	}

	return nil
}
