package models

import (
	"context"
	"fmt"
	"slices"
	"time"

	"github.com/apple/foundationdb/bindings/go/src/fdb"
	"github.com/apple/foundationdb/bindings/go/src/fdb/tuple"
	"github.com/bluesky-social/indigo/internal/cask/metrics"
	"github.com/bluesky-social/indigo/pkg/foundation"
	"github.com/bluesky-social/indigo/pkg/types"
	"go.opentelemetry.io/otel/attribute"
	"google.golang.org/protobuf/proto"
)

const (
	// Maximum number of events to delete in a single transaction
	cleanupBatchSize = 100
)

// deleteTarget represents an event that should be deleted
type deleteTarget struct {
	versionstamp []byte
	upstreamSeq  int64
	chunkCount   int
}

// CleanupOldEvents removes events older than the given retention duration.
// It returns the number of events deleted and any error encountered.
// This method is designed to be called periodically from a background goroutine.
func (m *Models) CleanupOldEvents(ctx context.Context, retention time.Duration) (deleted int, err error) {
	_, span, done := foundation.Observe(ctx, m.db, "CleanupOldEvents")
	defer func() { done(err) }()

	cutoff := time.Now().Add(-retention)
	span.SetAttributes(
		attribute.String("retention", retention.String()),
		attribute.String("cutoff", cutoff.Format(time.RFC3339)),
	)

	// Keep deleting batches until we're done or hit an error
	totalDeleted := 0
	batchCount := 0
	for {
		batchDeleted, batchDone, batchErr := m.cleanupBatch(ctx, cutoff)
		if batchErr != nil {
			span.SetAttributes(
				attribute.Int("deleted", totalDeleted),
				attribute.Int("batches", batchCount),
			)
			return totalDeleted, batchErr
		}
		totalDeleted += batchDeleted
		batchCount++
		if batchDone {
			break
		}
	}

	span.SetAttributes(
		attribute.Int("deleted", totalDeleted),
		attribute.Int("batches", batchCount),
	)
	metrics.EventsCleanedTotal.Add(float64(totalDeleted))

	// Update the oldest event age metric
	if err := m.updateOldestEventAge(ctx); err != nil {
		// Don't fail the cleanup for this, just log via span
		span.SetAttributes(attribute.String("oldest_age_error", err.Error()))
	}

	return totalDeleted, nil
}

// updateOldestEventAge reads the oldest event and updates the age metric
func (m *Models) updateOldestEventAge(ctx context.Context) error {
	oldestAge, err := m.GetOldestEventAge(ctx)
	if err != nil {
		return err
	}
	metrics.OldestEventAgeSeconds.Set(oldestAge.Seconds())
	return nil
}

// GetOldestEventAge returns the age of the oldest event in the database.
// Returns 0 if no events exist.
func (m *Models) GetOldestEventAge(ctx context.Context) (age time.Duration, err error) {
	_, span, done := foundation.Observe(ctx, m.db, "GetOldestEventAge")
	defer func() { done(err) }()

	var oldestTime time.Time
	oldestTime, err = foundation.ReadTransaction(m.db, func(tx fdb.ReadTransaction) (time.Time, error) {
		prefixLen := len(m.events.Bytes())

		// Read the first (oldest) event
		start := m.events.FDBKey()
		end := fdb.Key(append(m.events.Bytes(), 0xFF))
		keyRange := fdb.KeyRange{Begin: start, End: end}

		// Read enough keys to get all chunks of the first event
		iter := tx.GetRange(keyRange, fdb.RangeOptions{Limit: 256}).Iterator()

		var chunks [][]byte
		var targetVersionstamp []byte

		for iter.Advance() {
			kv, err := iter.Get()
			if err != nil {
				return time.Time{}, fmt.Errorf("failed to get event: %w", err)
			}

			// Extract cursor from key: [prefix][versionstamp (10)][event_index (1)][chunk_index (1)]
			if len(kv.Key) < prefixLen+cursorLength+1 {
				continue
			}
			versionstamp := kv.Key[prefixLen : prefixLen+cursorLength]

			if targetVersionstamp == nil {
				targetVersionstamp = versionstamp
			} else if !slices.Equal(versionstamp, targetVersionstamp) {
				// We've moved to a different event, stop
				break
			}

			chunks = append(chunks, kv.Value)
		}

		if len(chunks) == 0 {
			return time.Time{}, nil // no events
		}

		eventData, err := m.db.Chunker.ReassembleChunks(chunks)
		if err != nil {
			return time.Time{}, fmt.Errorf("failed to reassemble chunks: %w", err)
		}

		var event types.FirehoseEvent
		if err := proto.Unmarshal(eventData, &event); err != nil {
			return time.Time{}, fmt.Errorf("failed to unmarshal event: %w", err)
		}

		if event.ReceivedAt == nil {
			return time.Time{}, nil
		}
		return event.ReceivedAt.AsTime(), nil
	})
	if err != nil {
		return 0, err
	}

	if oldestTime.IsZero() {
		span.SetAttributes(attribute.Float64("oldest_age_seconds", 0))
		return 0, nil
	}

	age = time.Since(oldestTime)
	span.SetAttributes(attribute.Float64("oldest_age_seconds", age.Seconds()))
	return age, nil
}

// cleanupBatch deletes a batch of old events. Returns the count deleted,
// whether we're done (no more old events), and any error.
func (m *Models) cleanupBatch(ctx context.Context, cutoff time.Time) (deleted int, done bool, err error) {
	_, span, spanDone := foundation.Observe(ctx, m.db, "cleanupBatch")
	defer func() { spanDone(err) }()

	// First, read a batch of old events to find what needs deleting
	var targets []deleteTarget
	targets, err = foundation.ReadTransaction(m.db, func(tx fdb.ReadTransaction) ([]deleteTarget, error) {
		prefixLen := len(m.events.Bytes())

		// Scan events from oldest to newest
		start := m.events.FDBKey()
		end := fdb.Key(append(m.events.Bytes(), 0xFF))
		keyRange := fdb.KeyRange{Begin: start, End: end}

		// Read more keys than batch size since events may have multiple chunks
		iter := tx.GetRange(keyRange, fdb.RangeOptions{Limit: cleanupBatchSize * 10}).Iterator()

		var targets []deleteTarget
		var currentVersionstamp []byte
		var currentChunks [][]byte

		for iter.Advance() {
			kv, err := iter.Get()
			if err != nil {
				return nil, fmt.Errorf("failed to read event: %w", err)
			}

			// Extract cursor from key: [prefix][versionstamp (10)][event_index (1)][chunk_index (1)]
			if len(kv.Key) < prefixLen+cursorLength+1 {
				continue
			}
			versionstamp := kv.Key[prefixLen : prefixLen+cursorLength]

			// Check if this is a new event
			if currentVersionstamp != nil && !slices.Equal(versionstamp, currentVersionstamp) {
				// Process the previous event
				target, shouldDelete, err := m.checkEventForDeletion(currentVersionstamp, currentChunks, cutoff)
				if err != nil {
					return nil, err
				}
				if shouldDelete {
					targets = append(targets, target)
					if len(targets) >= cleanupBatchSize {
						return targets, nil
					}
				} else {
					// Hit an event within retention, we're done scanning
					return targets, nil
				}
				currentChunks = nil
			}

			currentVersionstamp = slices.Clone(versionstamp)
			currentChunks = append(currentChunks, kv.Value)
		}

		// Process the last event
		if len(currentChunks) > 0 {
			target, shouldDelete, err := m.checkEventForDeletion(currentVersionstamp, currentChunks, cutoff)
			if err != nil {
				return nil, err
			}
			if shouldDelete {
				targets = append(targets, target)
			}
		}

		return targets, nil
	})
	if err != nil {
		return 0, false, err
	}

	span.SetAttributes(attribute.Int("targets_found", len(targets)))

	if len(targets) == 0 {
		return 0, true, nil
	}

	// Now delete the identified events in a write transaction
	_, err = foundation.Transaction(m.db, func(tx fdb.Transaction) (any, error) {
		for _, target := range targets {
			// Delete all chunks for this event
			for i := range target.chunkCount {
				key := make([]byte, 0, len(m.events.Bytes())+cursorLength+1)
				key = append(key, m.events.Bytes()...)
				key = append(key, target.versionstamp...)
				key = append(key, byte(i))
				tx.Clear(fdb.Key(key))
			}

			// Delete the cursor index entry
			if target.upstreamSeq > 0 {
				cursorKey := m.cursorIndex.Pack(tuple.Tuple{target.upstreamSeq})
				tx.Clear(cursorKey)
			}
		}
		return nil, nil
	})
	if err != nil {
		return 0, false, fmt.Errorf("failed to delete events: %w", err)
	}

	// If we deleted fewer than batch size, we might be done
	done = len(targets) < cleanupBatchSize
	span.SetAttributes(
		attribute.Int("deleted", len(targets)),
		attribute.Bool("done", done),
	)
	return len(targets), done, nil
}

// checkEventForDeletion reassembles an event and checks if it should be deleted.
func (m *Models) checkEventForDeletion(versionstamp []byte, chunks [][]byte, cutoff time.Time) (deleteTarget, bool, error) {
	eventData, err := m.db.Chunker.ReassembleChunks(chunks)
	if err != nil {
		return deleteTarget{}, false, fmt.Errorf("failed to reassemble chunks: %w", err)
	}

	var event types.FirehoseEvent
	if err := proto.Unmarshal(eventData, &event); err != nil {
		return deleteTarget{}, false, fmt.Errorf("failed to unmarshal event: %w", err)
	}

	// Check if the event is older than the cutoff
	if event.ReceivedAt == nil || event.ReceivedAt.AsTime().After(cutoff) {
		return deleteTarget{}, false, nil
	}

	return deleteTarget{
		versionstamp: versionstamp,
		upstreamSeq:  event.UpstreamSeq,
		chunkCount:   len(chunks),
	}, true, nil
}
