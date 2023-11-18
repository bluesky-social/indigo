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
	lru "github.com/hashicorp/golang-lru/v2/expirable"
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

	DBs *lru.LRU[string, *litestream.DB]

	lk          sync.RWMutex
	DebounceSet map[string]time.Time
	SeenDBs     mapset.Set[string]

	watcherShutdown chan struct{}
}

func NewReplicator(config Config) *Replicator {
	r := Replicator{
		Config: config,

		DebounceSet: make(map[string]time.Time),
		SeenDBs:     mapset.NewSet[string](),
	}

	r.DBs = lru.NewLRU[string, *litestream.DB](config.MaxActiveDBs, r.onEvict, config.DBTTL)

	return &r
}

func (r *Replicator) Start() {
	// Watch directories for changes in a separate goroutine.
	shutdown := make(chan struct{})
	go func() {
		if err := r.watchDirs(r.Config.Dirs, shutdown); err != nil {
			slog.Error("failed to watch directories", "error", err)
		}
	}()

	r.watcherShutdown = shutdown
}

// Stop tears down the watcher and closes all open databases.
func (r *Replicator) Stop() (err error) {
	// Stop the watcher
	close(r.watcherShutdown)

	// Close all open databases
	err = r.shutdown()
	if err != nil {
		slog.Error("failed to shutdown", "error", err)
	}
	return err
}

func (r *Replicator) watchDirs(dirs []string, shutdown chan struct{}) error {
	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		return err
	}
	defer watcher.Close()

	log := slog.With("source", "watcher")

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

	for {
		select {
		case event, ok := <-watcher.Events:
			if !ok {
				return nil
			}
			// Only sync on file creation or modification of non `.` prefixed files with `.sqlite` suffix.
			startsWithDot := strings.HasPrefix(filepath.Base(event.Name), ".")
			endsInSQLite := strings.HasSuffix(filepath.Base(event.Name), ".sqlite")
			isWriteOp := event.Op&fsnotify.Write == fsnotify.Write
			isCreateOp := event.Op&fsnotify.Create == fsnotify.Create

			if (isWriteOp || isCreateOp) && !startsWithDot && endsInSQLite {
				log = log.With("filename", event.Name)
				log.Debug("file event", "event", event.Op.String())

				if unseen := r.SeenDBs.Add(event.Name); unseen {
					dbsSeenCounter.Inc()
				}

				err := r.processDBUpdate(event.Name)
				if err != nil {
					log.Error("failed to process DB Update", "error", err)
				}
			}
		case err, ok := <-watcher.Errors:
			if !ok {
				return nil
			}
			log.Error("watcher error", "error", err)
		case <-shutdown:
			return nil
		}
	}
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

func (r *Replicator) processDBUpdate(path string) error {
	if r.shouldDebounce(path) {
		// We're debouncing and don't need to do anything else
		return nil
	}

	// If the DB is already active, bump the TTL and return
	if db, ok := r.DBs.Get(path); ok {
		r.DBs.Add(path, db)
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

	if err := db.Open(); err != nil {
		return fmt.Errorf("failed to open DB for sync (%s): %w", path, err)
	}

	activeDBGauge.Inc()
	// Add to the active set, evicting the oldest DB if necessary.
	r.DBs.Add(path, db)

	return nil
}

// onEvict closes a DB and sets a debounce timer
func (r *Replicator) onEvict(path string, db *litestream.DB) {
	r.lk.Lock()
	defer r.lk.Unlock()
	r.DebounceSet[path] = time.Now().Add(time.Second * 8)
	if err := db.Close(); err != nil {
		slog.Error("failed to close DB on eviction", "error", err, "path", path)
	}
	activeDBGauge.Dec()
}

func (r *Replicator) shutdown() error {
	r.lk.Lock()
	defer r.lk.Unlock()

	slog.Warn("shutting down dir replication")

	for _, db := range r.DBs.Values() {
		if err := db.Close(); err != nil {
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
