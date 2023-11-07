package main

import (
	"fmt"
	"log/slog"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/benbjohnson/litestream"
	mapset "github.com/deckarep/golang-set/v2"
	"github.com/fsnotify/fsnotify"
)

type DBEntry struct {
	DB        *litestream.DB
	ExpiresAt time.Time
}

type Config struct {
	// List of directories to monitor
	Dirs []string
	// Root URL for directory replication
	ReplicaRoot string
	// Address for metrics server
	Addr string
	// DBTTL is the time to live for a database before it is closed.
	DBTTL time.Duration
	// SyncInterval is the interval at which active databases are synced.
	SyncInterval time.Duration
	// MaxActiveDBs is the maximum number of active databases to keep open.
	MaxActiveDBs int
}

// ReplicateCommand represents a command that continuously replicates SQLite databases.
type Replicator struct {
	Config Config

	lk          sync.RWMutex
	DBEntries   map[string]*DBEntry
	DebounceSet map[string]time.Time
	SeenDBs     mapset.Set[string]
}

func NewReplicator(config Config) *Replicator {
	return &Replicator{
		Config: config,

		DBEntries:   make(map[string]*DBEntry),
		DebounceSet: make(map[string]time.Time),
		SeenDBs:     mapset.NewSet[string](),
	}
}

// Close closes all open databases.
func (r *Replicator) Close() (err error) {
	err = r.shutdown()
	if err != nil {
		slog.Error("failed to shutdown", "error", err)
	}
	return err
}

func (r *Replicator) watchDirs(dirs []string, onUpdate func(string), shutdown chan struct{}) error {
	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		return err
	}
	defer watcher.Close()

	log := slog.With("source", "watcher")

	go func() {
		for {
			select {
			case event, ok := <-watcher.Events:
				if !ok {
					return
				}
				// Only sync on file creation or modification of non `.` prefixed files with `.sqlite` suffix.
				if (event.Op&fsnotify.Write == fsnotify.Write ||
					event.Op&fsnotify.Create == fsnotify.Create) &&
					!strings.HasPrefix(filepath.Base(event.Name), ".") &&
					strings.HasSuffix(event.Name, ".sqlite") {
					log.Debug("file modified", "filename", event.Name)

					if unseen := r.SeenDBs.Add(event.Name); unseen {
						dbsSeenCounter.Inc()
					}

					onUpdate(event.Name)
				}
			case err, ok := <-watcher.Errors:
				if !ok {
					return
				}
				log.Error("watcher error", "error", err)
			case <-shutdown:
				return
			}
		}
	}()

	// Add initial directories to the watcher.
	for _, dir := range dirs {
		err = filepath.WalkDir(dir, func(path string, d os.DirEntry, err error) error {
			if err != nil {
				return err
			}

			if d.IsDir() {
				return watcher.AddWith(path, fsnotify.WithBufferSize(1<<20))
			}
			return nil
		})
		if err != nil {
			return err
		}
	}

	<-shutdown
	return nil
}

// shouldDebounce returns true if the given path should be debounced
// and cleans up the debounce set if the debounce time has passed
func (r *Replicator) shouldDebounce(path string) bool {
	r.lk.Lock()
	defer r.lk.Unlock()
	debounceUntil, ok := r.DebounceSet[path]
	if ok {
		if time.Now().Before(debounceUntil) {
			return true
		}
		delete(r.DebounceSet, path)
	}
	return false
}

// syncNewDB starts syncing a new DB and closes the least recently used DB if necessary
// to make room in the active DB set
func (r *Replicator) syncNewDB(db *litestream.DB, path string) error {
	r.lk.Lock()
	defer r.lk.Unlock()

	_, err := r.evictLRU()
	if err != nil {
		return fmt.Errorf("failed to evict LRU DB: %w", err)
	}

	if err := db.Open(); err != nil {
		return fmt.Errorf("failed to open DB for sync (%s): %w", path, err)
	}

	activeDBGauge.Inc()

	r.DBEntries[path] = &DBEntry{
		DB:        db,
		ExpiresAt: time.Now().Add(r.Config.DBTTL),
	}

	return nil
}

// bumpTTL bumps the TTL of the given path if it exists
// and returns true if the TTL was bumped
func (r *Replicator) bumpTTL(path string) bool {
	r.lk.Lock()
	defer r.lk.Unlock()
	dbEntry, ok := r.DBEntries[path]
	if ok {
		dbEntry.ExpiresAt = time.Now().Add(r.Config.DBTTL)
		return true
	}
	return false
}

func (r *Replicator) processDBUpdate(path string) error {
	if r.shouldDebounce(path) {
		// We're debouncing and don't need to do anything else
		return nil
	}

	if r.bumpTTL(path) {
		// We've bumped the TTL for this active DB and don't need to do anything else
		return nil
	}

	slog.Warn("Syncing new active DB", "path", path)

	ep, err := url.Parse(r.Config.ReplicaRoot)
	if err != nil {
		return fmt.Errorf("failed to parse replica root URL (%s): %w", r.Config.ReplicaRoot, err)
	}

	dbConfig := DBConfig{Path: path}
	dbConfig.Replicas = append(dbConfig.Replicas, &ReplicaConfig{
		Type:         "s3",
		Endpoint:     fmt.Sprintf("%s://%s", ep.Scheme, ep.Host),
		Bucket:       strings.TrimPrefix(ep.Path, "/"),
		Path:         r.getRelativePath(path),
		SyncInterval: &r.Config.SyncInterval,
	})

	db, err := NewDBFromConfig(&dbConfig)
	if err != nil {
		return fmt.Errorf("failed to init DB from config for (%s): %w", path, err)
	}

	err = r.syncNewDB(db, path)
	if err != nil {
		return fmt.Errorf("failed to sync new DB (%s): %w", path, err)
	}

	return nil
}

func (r *Replicator) expireDBs() error {
	r.lk.Lock()
	defer r.lk.Unlock()

	for path, dbEntry := range r.DBEntries {
		if time.Now().After(dbEntry.ExpiresAt) {
			slog.Warn("closing expired DB", "path", path)
			if err := r.removeDB(dbEntry, path); err != nil {
				return fmt.Errorf("failed to remove expired DB (%s): %w", path, err)
			}
		}
	}

	return nil
}

// removeDB removes the given DB from the active DB set and sets a debounce timer
// ONLY CALL THIS WITH THE LOCK HELD
func (r *Replicator) removeDB(entry *DBEntry, path string) error {
	delete(r.DBEntries, path)
	r.DebounceSet[path] = time.Now().Add(time.Second * 8)
	if err := entry.DB.Close(); err != nil {
		return fmt.Errorf("failed to close DB (%s): %w", path, err)
	}
	activeDBGauge.Dec()
	return nil
}

// evictLRU evicts the least recently used DB if the active DB set is full
// and returns true if an eviction occurred
// ONLY CALL THIS WITH THE LOCK HELD
func (r *Replicator) evictLRU() (bool, error) {
	// Check if we need to close an existing DB.
	if len(r.DBEntries) >= r.Config.MaxActiveDBs {
		// Close the least recently used DB.
		var lruPath string
		var lruExpiresAt time.Time
		for path, dbEntry := range r.DBEntries {
			if lruExpiresAt.IsZero() || dbEntry.ExpiresAt.Before(lruExpiresAt) {
				lruPath = path
				lruExpiresAt = dbEntry.ExpiresAt
			}
		}
		slog.Warn("closing least recently used DB", "path", lruPath, "expires_at", lruExpiresAt)
		if err := r.removeDB(r.DBEntries[lruPath], lruPath); err != nil {
			return false, fmt.Errorf("failed to remove least recently used DB (%s): %w", lruPath, err)
		}
		return true, nil
	}
	return false, nil
}

func (r *Replicator) shutdown() error {
	r.lk.Lock()
	defer r.lk.Unlock()

	slog.Warn("shutting down dir replication")

	for _, dbEntry := range r.DBEntries {
		if err := dbEntry.DB.Close(); err != nil {
			slog.Error("failed to close DB", "error", err)
		}
	}

	slog.Warn("all DBs closed")

	return nil
}

func (r *Replicator) getRelativePath(path string) string {
	// Match the longest prefix of the path to one of the configured directories.
	var dir string
	for _, d := range r.Config.Dirs {
		if strings.HasPrefix(path, d) && len(d) > len(dir) {
			dir = d
		}
	}

	// If no directory matched, return the full path.
	if dir == "" {
		return path
	}

	// Otherwise, return the relative path.
	return strings.TrimPrefix(
		strings.TrimPrefix(path, dir),
		"/")
}
