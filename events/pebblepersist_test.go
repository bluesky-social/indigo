package events

import (
	"gorm.io/gorm"
	"path/filepath"
	"testing"
)

func TestPebblePersist(t *testing.T) {
	factory := func(tempPath string, db *gorm.DB) (EventPersistence, error) {
		opts := DefaultPebblePersistOptions
		opts.DbPath = filepath.Join(tempPath, "pebble.db")
		return NewPebblePersistance(&opts)
	}
	testPersister(t, factory)
}
