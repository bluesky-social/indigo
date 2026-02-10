package models

import (
	"fmt"

	"github.com/apple/foundationdb/bindings/go/src/fdb/directory"
	"github.com/bluesky-social/indigo/pkg/foundation"
)

type Models struct {
	db *foundation.DB

	// Primary index: events keyed by versionstamp for global ordering
	// Key: (versionstamp), Value: serialized FirehoseEvent
	events directory.DirectorySubspace

	// Secondary index: maps upstream sequence number to cursor
	// Key: (upstream_seq as big-endian int64), Value: cursor (11 bytes: versionstamp + event_index)
	// This allows O(1) lookup for cursor positioning instead of O(n) scan
	cursorIndex directory.DirectorySubspace
}

func New(db *foundation.DB) (*Models, error) {
	return NewWithPrefix(db, "")
}

func NewWithPrefix(db *foundation.DB, prefix string) (*Models, error) {
	m := &Models{
		db: db,
	}

	// Build directory paths, optionally prefixed for test isolation
	eventsPath := []string{"firehose_events"}
	cursorIndexPath := []string{"cursor_index"}
	if prefix != "" {
		eventsPath = []string{prefix, "firehose_events"}
		cursorIndexPath = []string{prefix, "cursor_index"}
	}

	var err error
	m.events, err = directory.CreateOrOpen(m.db.Database, eventsPath, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create firehose_events directory: %w", err)
	}

	m.cursorIndex, err = directory.CreateOrOpen(m.db.Database, cursorIndexPath, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create cursor_index directory: %w", err)
	}

	return m, nil
}
