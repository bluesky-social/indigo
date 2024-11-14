package events

import (
	"gorm.io/gorm"
	"path/filepath"
	"testing"
)

func TestPebblePersist(t *testing.T) {
	factory := func(tempPath string, db *gorm.DB) (EventPersistence, error) {
		return NewPebblePersistance(filepath.Join(tempPath, "pebble.db"))
	}
	testPersister(t, factory)
}
