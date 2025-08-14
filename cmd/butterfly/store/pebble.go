// Package store provides a pebble implementation of the Store interface
package store

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"github.com/bluesky-social/indigo/cmd/butterfly/remote"
	"github.com/cockroachdb/pebble"
)

// PebbleStore implements Store using CockroachDB's Pebble embedded key-value store
type PebbleStore struct {
	db   *pebble.DB
	path string

	// Batch processing
	batchMu    sync.Mutex
	batch      *pebble.Batch
	batchSize  int
	maxBatch   int
	flushTimer *time.Timer
}

// NewPebbleStore creates a new Pebble-backed store
func NewPebbleStore(path string) *PebbleStore {
	return &PebbleStore{
		path:     path,
		maxBatch: 1000, // Default batch size
	}
}

// Setup initializes the Pebble database
func (s *PebbleStore) Setup(ctx context.Context) error {
	// Configure Pebble
	opts := &pebble.Options{
		// Set cache size to 64MB
		// Cache: pebble.NewCache(64 << 20),
		// // Enable compression
		// Levels: []pebble.LevelOptions{
		// 	{Compression: pebble.SnappyCompression},
		// },
		// // Set write buffer size
		// MemTableSize: 64 << 20,
		// // Configure compaction
		// L0CompactionThreshold: 2,
		// L0StopWritesThreshold: 12,
	}
	// defer opts.Cache.Unref()

	// Open the database
	db, err := pebble.Open(s.path, opts)
	if err != nil {
		return fmt.Errorf("failed to open pebble database: %w", err)
	}

	s.db = db

	// Initialize batch
	s.batch = s.db.NewBatch()

	return nil
}

// Close flushes pending writes and closes the database
func (s *PebbleStore) Close() error {
	fmt.Println()
	// Flush any pending batch
	if err := s.flushBatch(); err != nil {
		fmt.Printf("Warning: failed to flush batch on close: %v\n", err)
	}

	// Cancel flush timer if active
	if s.flushTimer != nil {
		s.flushTimer.Stop()
	}

	// Close the database
	if s.db != nil {
		return s.db.Close()
	}

	return nil
}

// BackfillRepo resets a repo and re-ingests it from a remote stream
func (s *PebbleStore) BackfillRepo(ctx context.Context, did string, stream *remote.RemoteStream) error {
	// Delete all existing data for this repo
	if err := s.deleteRepo(did); err != nil {
		return fmt.Errorf("failed to delete existing repo data: %w", err)
	}

	// Process the stream
	return s.ActiveSync(ctx, stream)
}

// ActiveSync processes live update events from a remote stream
func (s *PebbleStore) ActiveSync(ctx context.Context, stream *remote.RemoteStream) error {
	for event := range stream.Ch {
		select {
		case <-ctx.Done():
			// Flush pending batch before returning
			if err := s.flushBatch(); err != nil {
				return fmt.Errorf("failed to flush batch: %w", err)
			}
			return ctx.Err()
		default:
		}

		if err := s.processEvent(event); err != nil {
			// Log error but continue processing
			fmt.Printf("Error processing event for %s: %v\n", event.Did, err)
		}

		// Auto-flush batch if it gets too large
		s.batchMu.Lock()
		if s.batchSize >= s.maxBatch {
			if err := s.flushBatchLocked(); err != nil {
				s.batchMu.Unlock()
				return fmt.Errorf("failed to flush batch: %w", err)
			}
		}
		s.batchMu.Unlock()
	}

	// Final flush
	return s.flushBatch()
}

// processEvent handles a single stream event
func (s *PebbleStore) processEvent(event remote.StreamEvent) error {
	switch event.Kind {
	case remote.EventKindCommit:
		return s.processCommit(event.Did, event.Commit)
	case remote.EventKindError:
		return s.processError(event.Did, event.Error)
	default:
		return fmt.Errorf("unknown event kind: %s", event.Kind)
	}
}

// processCommit handles commit events
func (s *PebbleStore) processCommit(did string, commit *remote.StreamEventCommit) error {
	if commit == nil {
		return fmt.Errorf("nil commit event")
	}

	// Build the key for this record
	key := s.buildRecordKey(did, commit.Collection, commit.Rkey)

	s.batchMu.Lock()
	defer s.batchMu.Unlock()

	switch commit.Operation {
	case remote.OpCreate, remote.OpUpdate:
		// Serialize the record
		value, err := json.Marshal(commit.Record)
		if err != nil {
			return fmt.Errorf("failed to marshal record: %w", err)
		}

		// Add to batch
		if err := s.batch.Set([]byte(key), value, pebble.Sync); err != nil {
			return fmt.Errorf("failed to set record: %w", err)
		}

	case remote.OpDelete:
		// Delete from batch
		if err := s.batch.Delete([]byte(key), pebble.Sync); err != nil {
			return fmt.Errorf("failed to delete record: %w", err)
		}

	default:
		return fmt.Errorf("unknown operation: %s", commit.Operation)
	}

	// Store commit metadata
	commitKey := s.buildCommitKey(did, commit.Rev)
	commitData, err := json.Marshal(commit)
	if err != nil {
		return fmt.Errorf("failed to marshal commit: %w", err)
	}
	if err := s.batch.Set([]byte(commitKey), commitData, pebble.Sync); err != nil {
		return fmt.Errorf("failed to store commit: %w", err)
	}

	s.batchSize++

	// Schedule a flush if timer not already running
	if s.flushTimer == nil {
		s.flushTimer = time.AfterFunc(5*time.Second, func() {
			s.flushBatch()
		})
	}

	return nil
}

// processError handles error events
func (s *PebbleStore) processError(did string, streamErr *remote.StreamEventError) error {
	if streamErr == nil {
		return fmt.Errorf("nil error event")
	}

	// Log the error
	fmt.Printf("Stream error for %s: %v (fatal=%v)\n", did, streamErr.Err, streamErr.Fatal)

	if streamErr.Fatal {
		return streamErr.Err
	}

	return nil
}

// KV Storage Methods

// KvGet retrieves a value from the KV namespace
func (s *PebbleStore) KvGet(namespace string, key string) (string, error) {
	fullKey := s.buildKvKey(namespace, key)
	value, closer, err := s.db.Get([]byte(fullKey))
	if err != nil {
		if err == pebble.ErrNotFound {
			return "", fmt.Errorf("key %q not found in namespace %q", key, namespace)
		}
		return "", fmt.Errorf("failed to get key: %w", err)
	}
	defer closer.Close()

	return string(value), nil
}

// KvPut stores a value in the KV namespace
func (s *PebbleStore) KvPut(namespace string, key string, value string) error {
	fullKey := s.buildKvKey(namespace, key)
	return s.db.Set([]byte(fullKey), []byte(value), pebble.Sync)
}

// KvDel deletes a value from the KV namespace
func (s *PebbleStore) KvDel(namespace string, key string) error {
	fullKey := s.buildKvKey(namespace, key)
	err := s.db.Delete([]byte(fullKey), pebble.Sync)
	if err != nil && err != pebble.ErrNotFound {
		return fmt.Errorf("failed to delete key: %w", err)
	}
	return nil
}

// Helper methods

// buildRecordKey constructs a key for a record
func (s *PebbleStore) buildRecordKey(did, collection, rkey string) string {
	return fmt.Sprintf("record/%s/%s/%s", did, collection, rkey)
}

// buildCommitKey constructs a key for a commit
func (s *PebbleStore) buildCommitKey(did, rev string) string {
	return fmt.Sprintf("commit/%s/%s", did, rev)
}

// buildKvKey constructs a key for KV storage
func (s *PebbleStore) buildKvKey(namespace, key string) string {
	return fmt.Sprintf("kv/%s/%s", namespace, key)
}

// deleteRepo removes all data for a repository
func (s *PebbleStore) deleteRepo(did string) error {
	// Delete all records with prefix
	prefix := fmt.Sprintf("record/%s/", did)
	if err := s.deleteByPrefix(prefix); err != nil {
		return err
	}

	// Delete all commits
	prefix = fmt.Sprintf("commit/%s/", did)
	if err := s.deleteByPrefix(prefix); err != nil {
		return err
	}

	return nil
}

// deleteByPrefix deletes all keys with the given prefix
func (s *PebbleStore) deleteByPrefix(prefix string) error {
	iter, err := s.db.NewIter(&pebble.IterOptions{
		LowerBound: []byte(prefix),
		UpperBound: []byte(prefix + "\xff"),
	})
	if err != nil {
		return fmt.Errorf("failed to create iterator: %w", err)
	}
	defer iter.Close()

	batch := s.db.NewBatch()
	defer batch.Close()

	count := 0
	for iter.First(); iter.Valid(); iter.Next() {
		if err := batch.Delete(iter.Key(), pebble.Sync); err != nil {
			return err
		}
		count++

		// Commit batch periodically
		if count%1000 == 0 {
			if err := batch.Commit(pebble.Sync); err != nil {
				return err
			}
			batch = s.db.NewBatch()
		}
	}

	// Commit remaining
	if count%1000 != 0 {
		if err := batch.Commit(pebble.Sync); err != nil {
			return err
		}
	}

	return iter.Error()
}

// flushBatch commits the current batch
func (s *PebbleStore) flushBatch() error {
	s.batchMu.Lock()
	defer s.batchMu.Unlock()
	return s.flushBatchLocked()
}

// flushBatchLocked commits the current batch (must be called with batchMu locked)
func (s *PebbleStore) flushBatchLocked() error {
	if s.batch == nil || s.batchSize == 0 {
		return nil
	}

	if err := s.batch.Commit(pebble.Sync); err != nil {
		return err
	}

	// Reset batch
	s.batch = s.db.NewBatch()
	s.batchSize = 0

	// Cancel timer if running
	if s.flushTimer != nil {
		s.flushTimer.Stop()
		s.flushTimer = nil
	}

	return nil
}

// GetRecord retrieves a single record
func (s *PebbleStore) GetRecord(ctx context.Context, did, collection, rkey string) (map[string]any, error) {
	// Build the key for this record
	key := s.buildRecordKey(did, collection, rkey)

	// Get the record from the database
	value, closer, err := s.db.Get([]byte(key))
	if err != nil {
		if err == pebble.ErrNotFound {
			return nil, fmt.Errorf("record not found: %s/%s/%s", did, collection, rkey)
		}
		return nil, fmt.Errorf("failed to get record: %w", err)
	}
	defer closer.Close()

	// Unmarshal the record
	var record map[string]any
	if err := json.Unmarshal(value, &record); err != nil {
		return nil, fmt.Errorf("failed to unmarshal record: %w", err)
	}

	return record, nil
}

// ListRecords retrieves records for a given DID and collection
func (s *PebbleStore) ListRecords(ctx context.Context, did, collection string, limit int) ([]map[string]any, error) {
	// Build the prefix for this collection
	prefix := fmt.Sprintf("record/%s/%s/", did, collection)

	// Create iterator with the prefix bounds
	iter, err := s.db.NewIter(&pebble.IterOptions{
		LowerBound: []byte(prefix),
		UpperBound: []byte(prefix + "\xff"),
	})
	if err != nil {
		return nil, fmt.Errorf("failed to create iterator: %w", err)
	}
	defer iter.Close()

	// Collect records
	var records []map[string]any
	count := 0

	for iter.First(); iter.Valid(); iter.Next() {
		// Check context cancellation
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		default:
		}

		// Check limit
		if limit > 0 && count >= limit {
			break
		}

		// Get the value
		value := iter.Value()

		// Unmarshal the record
		var record map[string]any
		if err := json.Unmarshal(value, &record); err != nil {
			// Log error but continue to next record
			fmt.Printf("Warning: failed to unmarshal record at key %s: %v\n", string(iter.Key()), err)
			continue
		}

		// Add metadata about the record key
		// Extract the rkey from the full key (record/did/collection/rkey)
		keyStr := string(iter.Key())
		if len(keyStr) > len(prefix) {
			rkey := keyStr[len(prefix):]
			record["_rkey"] = rkey
		}

		records = append(records, record)
		count++
	}

	// Check for iterator errors
	if err := iter.Error(); err != nil {
		return nil, fmt.Errorf("iterator error: %w", err)
	}

	return records, nil
}

// ListAllRecords retrieves all records for a given DID across all collections
func (s *PebbleStore) ListAllRecords(ctx context.Context, did string, limit int) (map[string][]map[string]any, error) {
	// Build the prefix for all records of this DID
	prefix := fmt.Sprintf("record/%s/", did)

	// Create iterator with the prefix bounds
	iter, err := s.db.NewIter(&pebble.IterOptions{
		LowerBound: []byte(prefix),
		UpperBound: []byte(prefix + "\xff"),
	})
	if err != nil {
		return nil, fmt.Errorf("failed to create iterator: %w", err)
	}
	defer iter.Close()

	// Collect records organized by collection
	result := make(map[string][]map[string]any)
	count := 0

	for iter.First(); iter.Valid(); iter.Next() {
		// Check context cancellation
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		default:
		}

		// Check limit
		if limit > 0 && count >= limit {
			break
		}

		// Parse the key to extract collection
		keyStr := string(iter.Key())
		// Key format: record/did/collection/rkey
		parts := []byte(keyStr)

		// Find collection name
		collectionStart := len(prefix)
		collectionEnd := collectionStart
		for i := collectionStart; i < len(parts); i++ {
			if parts[i] == '/' {
				collectionEnd = i
				break
			}
		}

		if collectionEnd == collectionStart {
			continue // Invalid key format
		}

		collection := string(parts[collectionStart:collectionEnd])
		rkey := string(parts[collectionEnd+1:])

		// Get the value
		value := iter.Value()

		// Unmarshal the record
		var record map[string]any
		if err := json.Unmarshal(value, &record); err != nil {
			// Log error but continue to next record
			fmt.Printf("Warning: failed to unmarshal record at key %s: %v\n", keyStr, err)
			continue
		}

		// Add metadata
		record["_rkey"] = rkey
		record["_collection"] = collection

		// Add to result
		if _, exists := result[collection]; !exists {
			result[collection] = []map[string]any{}
		}
		result[collection] = append(result[collection], record)
		count++
	}

	// Check for iterator errors
	if err := iter.Error(); err != nil {
		return nil, fmt.Errorf("iterator error: %w", err)
	}

	return result, nil
}
