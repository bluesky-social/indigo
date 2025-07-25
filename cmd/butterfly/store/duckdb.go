// Package store provides a duckdb implementation of the Store interface
package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	_ "github.com/marcboeker/go-duckdb"

	"github.com/bluesky-social/indigo/cmd/butterfly/remote"
)

// DuckdbStore implements Store by storing repository data in DuckDB
type DuckdbStore struct {
	// The filepath to store the duckdb file
	Dbpath string

	// Database connection
	db *sql.DB

	// Prepared statements for performance
	insertStmt *sql.Stmt
	deleteStmt *sql.Stmt

	// Mutex for thread safety
	mu sync.Mutex
}

// NewDuckdbStore creates a new DuckdbStore
func NewDuckdbStore(dbpath string) *DuckdbStore {
	return &DuckdbStore{
		Dbpath: dbpath,
	}
}

// Setup creates the database if it doesn't exist
// If it does exist, ensures the needed tables and indexes are created
func (d *DuckdbStore) Setup(ctx context.Context) error {
	// Ensure parent directory exists
	dir := filepath.Dir(d.Dbpath)
	if dir != "" && dir != "." {
		if err := os.MkdirAll(dir, 0755); err != nil {
			return fmt.Errorf("failed to create directory: %w", err)
		}
	}

	// Open database connection
	db, err := sql.Open("duckdb", d.Dbpath)
	if err != nil {
		return fmt.Errorf("failed to open database: %w", err)
	}
	d.db = db

	// Create main records table
	createTableSQL := `
	CREATE TABLE IF NOT EXISTS records (
		did VARCHAR NOT NULL,
		collection VARCHAR NOT NULL,
		rkey VARCHAR NOT NULL,
		cid VARCHAR,
		rev VARCHAR,
		record JSON,
		deleted BOOLEAN DEFAULT false,
		created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
		updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
		PRIMARY KEY (did, collection, rkey)
	)`

	if _, err := d.db.ExecContext(ctx, createTableSQL); err != nil {
		return fmt.Errorf("failed to create records table: %w", err)
	}

	// Create indexes for better query performance
	indexes := []string{
		"CREATE INDEX IF NOT EXISTS idx_did ON records(did)",
		"CREATE INDEX IF NOT EXISTS idx_collection ON records(did, collection)",
		"CREATE INDEX IF NOT EXISTS idx_collection ON records(did, collection, rkey)",
	}

	for _, idx := range indexes {
		if _, err := d.db.ExecContext(ctx, idx); err != nil {
			return fmt.Errorf("failed to create index: %w", err)
		}
	}

	// Prepare statements
	d.insertStmt, err = d.db.PrepareContext(ctx, `
		INSERT INTO records (did, collection, rkey, cid, rev, record, deleted, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, false, ?)
		ON CONFLICT (did, collection, rkey) DO UPDATE SET
			cid = excluded.cid,
			rev = excluded.rev,
			record = excluded.record,
			deleted = false,
			updated_at = excluded.updated_at
	`)
	if err != nil {
		return fmt.Errorf("failed to prepare insert statement: %w", err)
	}

	d.deleteStmt, err = d.db.PrepareContext(ctx, `
		UPDATE records 
		SET deleted = true, updated_at = ?
		WHERE did = ? AND collection = ? AND rkey = ?
	`)
	if err != nil {
		return fmt.Errorf("failed to prepare delete statement: %w", err)
	}

	return nil
}

// Close cleans up database connection and prepared statements
func (d *DuckdbStore) Close() error {
	d.mu.Lock()
	defer d.mu.Unlock()

	var errs []error

	if d.insertStmt != nil {
		if err := d.insertStmt.Close(); err != nil {
			errs = append(errs, fmt.Errorf("failed to close insert statement: %w", err))
		}
	}

	if d.deleteStmt != nil {
		if err := d.deleteStmt.Close(); err != nil {
			errs = append(errs, fmt.Errorf("failed to close delete statement: %w", err))
		}
	}

	if d.db != nil {
		if err := d.db.Close(); err != nil {
			errs = append(errs, fmt.Errorf("failed to close database: %w", err))
		}
	}

	if len(errs) > 0 {
		return fmt.Errorf("errors closing DuckDB store: %v", errs)
	}
	return nil
}

// Receive processes events from the stream
func (d *DuckdbStore) Receive(ctx context.Context, stream *remote.RemoteStream) error {
	// Start a transaction for better performance with batch operations
	tx, err := d.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("failed to begin transaction: %w", err)
	}

	// Prepare transactional statements
	insertTxStmt := tx.StmtContext(ctx, d.insertStmt)
	deleteTxStmt := tx.StmtContext(ctx, d.deleteStmt)

	batchSize := 0
	const maxBatchSize = 1000

	for event := range stream.Ch {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		if event.Kind != remote.EventKindCommit || event.Commit == nil {
			continue
		}

		if err := d.processCommit(ctx, event.Did, event.Commit, insertTxStmt, deleteTxStmt); err != nil {
			// Log error but continue processing
			fmt.Fprintf(os.Stderr, "duckdb: error processing commit for %s: %v\n", event.Did, err)
			continue
		}

		batchSize++
		// Commit transaction periodically for better performance
		if batchSize >= maxBatchSize {
			if err := tx.Commit(); err != nil {
				return fmt.Errorf("failed to commit transaction: %w", err)
			}

			// Start new transaction
			tx, err = d.db.BeginTx(ctx, nil)
			if err != nil {
				return fmt.Errorf("failed to begin new transaction: %w", err)
			}

			insertTxStmt = tx.StmtContext(ctx, d.insertStmt)
			deleteTxStmt = tx.StmtContext(ctx, d.deleteStmt)

			batchSize = 0
		}
	}

	// Commit any remaining operations
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("failed to commit final transaction: %w", err)
	}

	return nil
}

// processCommit handles a single commit event
func (d *DuckdbStore) processCommit(ctx context.Context, did string, commit *remote.StreamEventCommit, insertStmt, deleteStmt *sql.Stmt) error {
	now := time.Now()

	switch commit.Operation {
	case remote.OpCreate, remote.OpUpdate:
		// Marshal record to JSON
		recordJSON, err := json.Marshal(commit.Record)
		if err != nil {
			return fmt.Errorf("failed to marshal record: %w", err)
		}

		// Use insert with ON CONFLICT for both create and update
		_, err = insertStmt.ExecContext(ctx,
			did, commit.Collection, commit.Rkey,
			commit.Cid, commit.Rev, string(recordJSON), now)
		if err != nil {
			return fmt.Errorf("failed to insert/update record: %w", err)
		}

	case remote.OpDelete:
		// Soft delete by setting deleted flag
		_, err := deleteStmt.ExecContext(ctx,
			now, did, commit.Collection, commit.Rkey)
		if err != nil {
			return fmt.Errorf("failed to delete record: %w", err)
		}

	default:
		return fmt.Errorf("unknown operation: %s", commit.Operation)
	}

	return nil
}

// Query methods for retrieving data from DuckDB

// GetRecord retrieves a single record
func (d *DuckdbStore) GetRecord(ctx context.Context, did, collection, rkey string) (map[string]any, error) {
	d.mu.Lock()
	defer d.mu.Unlock()

	var record map[string]any
	var deleted bool

	err := d.db.QueryRowContext(ctx,
		"SELECT record, deleted FROM records WHERE did = ? AND collection = ? AND rkey = ?",
		did, collection, rkey).Scan(&record, &deleted)

	if err == sql.ErrNoRows || deleted {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("failed to query record: %w", err)
	}

	return record, nil
}

// ListRecords retrieves all records for a given DID and collection
func (d *DuckdbStore) ListRecords(ctx context.Context, did, collection string, limit int) ([]map[string]any, error) {
	d.mu.Lock()
	defer d.mu.Unlock()

	query := "SELECT rkey, record FROM records WHERE did = ? AND collection = ? AND deleted = false"
	args := []any{did, collection}

	if limit > 0 {
		query += " LIMIT ?"
		args = append(args, limit)
	}

	rows, err := d.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("failed to query records: %w", err)
	}
	defer rows.Close()

	var results []map[string]any
	for rows.Next() {
		var rkey string
		var record map[string]any
		if err := rows.Scan(&rkey, &record); err != nil {
			return nil, fmt.Errorf("failed to scan row: %w", err)
		}

		// Add rkey to the record for reference
		record["_rkey"] = rkey
		results = append(results, record)
	}

	return results, rows.Err()
}

// GetStats returns statistics about the stored data
func (d *DuckdbStore) GetStats(ctx context.Context) (map[string]any, error) {
	d.mu.Lock()
	defer d.mu.Unlock()

	stats := make(map[string]any)

	// Total records
	var totalRecords int64
	err := d.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM records WHERE deleted = false").Scan(&totalRecords)
	if err != nil {
		return nil, fmt.Errorf("failed to count records: %w", err)
	}
	stats["total_records"] = totalRecords

	// Records by collection
	rows, err := d.db.QueryContext(ctx, `
		SELECT collection, COUNT(*) as count 
		FROM records 
		WHERE deleted = false 
		GROUP BY collection 
		ORDER BY count DESC
	`)
	if err != nil {
		return nil, fmt.Errorf("failed to query collection stats: %w", err)
	}
	defer rows.Close()

	collections := make(map[string]int64)
	for rows.Next() {
		var collection string
		var count int64
		if err := rows.Scan(&collection, &count); err != nil {
			return nil, fmt.Errorf("failed to scan collection stats: %w", err)
		}
		collections[collection] = count
	}
	stats["collections"] = collections

	// Unique DIDs
	var uniqueDIDs int64
	err = d.db.QueryRowContext(ctx, "SELECT COUNT(DISTINCT did) FROM records WHERE deleted = false").Scan(&uniqueDIDs)
	if err != nil {
		return nil, fmt.Errorf("failed to count unique DIDs: %w", err)
	}
	stats["unique_dids"] = uniqueDIDs

	return stats, nil
}
