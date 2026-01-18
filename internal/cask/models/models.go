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
}

func New(db *foundation.DB) (*Models, error) {
	return NewWithPrefix(db, "")
}

func NewWithPrefix(db *foundation.DB, prefix string) (*Models, error) {
	m := &Models{
		db: db,
	}

	// Build directory path, optionally prefixed for test isolation
	dirPath := []string{"firehose_events"}
	if prefix != "" {
		dirPath = []string{prefix, "firehose_events"}
	}

	var err error
	m.events, err = directory.CreateOrOpen(m.db.Database, dirPath, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create firehose_events directory: %w", err)
	}

	return m, nil
}
