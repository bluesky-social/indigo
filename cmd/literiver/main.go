package main

import (
	"fmt"
	"log/slog"
	"net"
	"net/http"
	_ "net/http/pprof"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/benbjohnson/litestream"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/urfave/cli/v2"
)

func main() {
	app := &cli.App{
		Name:  "literiver",
		Usage: "Replicate SQLite databases in a directory to S3",
		Flags: []cli.Flag{
			&cli.StringFlag{
				Name:    "addr",
				Usage:   "Address to serve metrics on",
				EnvVars: []string{"LITERIVER_ADDR"},
				Value:   "0.0.0.0:9032",
			},
			&cli.StringFlag{
				Name:    "log-level",
				Usage:   "Log level (debug, info, warn, error)",
				EnvVars: []string{"LITERIVER_LOG_LEVEL"},
				Value:   "warn",
			},
		},
		Commands: []*cli.Command{
			{
				Name:   "restore",
				Usage:  "Restore a local copy of databases from an S3 bucket",
				Action: Restore,
				Flags: []cli.Flag{
					&cli.StringFlag{
						Name:     "cloned-bucket-root",
						Usage:    "local directory that contains a clone of the s3 bucket",
						EnvVars:  []string{"LITERIVER_RESTORE_CLONED_BUCKET_ROOT"},
						Required: true,
					},
					&cli.StringFlag{
						Name:     "target-directory",
						Usage:    "local directory to restore the databases to",
						EnvVars:  []string{"LITERIVER_RESTORE_TARGET_DIRECTORY"},
						Required: true,
					},
					&cli.IntFlag{
						Name:    "concurrency",
						Usage:   "number of concurrent restores to run.",
						Value:   10,
						EnvVars: []string{"LITERIVER_RESTORE_CONCURRENCY"},
					},
				},
			},
			{
				Name:   "replicate",
				Usage:  "Replicate databases from a directory to S3",
				Action: Replicate,
				Flags: []cli.Flag{
					&cli.StringSliceFlag{
						Name:     "dir",
						Usage:    "Directories to monitor (can be specified multiple times)",
						EnvVars:  []string{"LITERIVER_DIR"},
						Required: true,
					},
					&cli.StringFlag{
						Name:     "replica-root",
						Usage:    "S3 Bucket URL for Replication (https://{s3_url}.com/{bucket_name})",
						EnvVars:  []string{"LITERIVER_REPLICA_ROOT"},
						Required: true,
					},
					&cli.DurationFlag{
						Name:    "db-ttl",
						Usage:   "Time to live for a database before it is closed",
						EnvVars: []string{"LITERIVER_DB_TTL"},
						Value:   2 * time.Minute,
					},
					&cli.DurationFlag{
						Name:    "sync-interval",
						Usage:   "How frequently active DBs should be synced",
						EnvVars: []string{"LITERIVER_SYNC_INTERVAL"},
						Value:   5 * time.Second,
					},
					&cli.IntFlag{
						Name:    "max-active-dbs",
						Usage:   "Maximum number of active databases to keep open, least recently used will be closed first",
						EnvVars: []string{"LITERIVER_MAX_ACTIVE_DBS"},
						Value:   2_000,
					},
				},
			},
		},
		DefaultCommand: "replicate",
	}

	if err := app.Run(os.Args); err != nil {
		slog.Error("failed to run", "error", err)
	}

	slog.Info("literiver exiting")
}

type replicaOpt struct {
	replica *litestream.Replica
	opt     *litestream.RestoreOptions
	source  string
}

// Restore runs on an rclone'd directory and restores all databases from S3.
func Restore(cctx *cli.Context) (err error) {
	ctx := cctx.Context
	start := time.Now()

	logLvl := new(slog.LevelVar)
	logLvl.UnmarshalText([]byte(cctx.String("log-level")))
	slog.SetDefault(slog.New(slog.NewJSONHandler(os.Stderr, &slog.HandlerOptions{
		Level: logLvl,
	})))

	// Walk all databases in the directory
	// For each database, register a new DB using the path as a local replica target

	var dbPaths []string

	clonedBucketRoot, err := filepath.Abs(cctx.String("cloned-bucket-root"))
	if err != nil {
		return fmt.Errorf("failed to resolve directory path (%s): %w", clonedBucketRoot, err)
	}

	targetDir, err := filepath.Abs(cctx.String("target-directory"))
	if err != nil {
		return fmt.Errorf("failed to resolve directory path (%s): %w", targetDir, err)
	}

	slog.Warn("starting restore", "cloned_bucket_root", clonedBucketRoot, "target_directory", targetDir, "concurrency", cctx.Int("concurrency"))

	// Check if clonedBucketRoot exists
	if _, err := os.Stat(clonedBucketRoot); os.IsNotExist(err) {
		return fmt.Errorf("cloned bucket root does not exist: %s", clonedBucketRoot)
	}

	// Create targetDir if it doesn't exist
	if _, err := os.Stat(targetDir); os.IsNotExist(err) {
		slog.Warn("creating target directory", "target_directory", targetDir)
		if err := os.MkdirAll(targetDir, 0755); err != nil {
			return fmt.Errorf("failed to create target directory: %w", err)
		}
	}

	// Function to be called for each file/directory found
	err = filepath.Walk(clonedBucketRoot, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}

		// Check if the file is a directory and its name ends with '.sqlite'
		if info.IsDir() && strings.HasSuffix(info.Name(), ".sqlite") {
			dbPaths = append(dbPaths, path)
		}

		return nil
	})

	if err != nil {
		return fmt.Errorf("error walking the path %q: %v", clonedBucketRoot, err)
	}

	slog.Warn("found databases", "databases", dbPaths)

	replicas := []replicaOpt{}

	replicaInitErrors := []error{}

	for _, dbPath := range dbPaths {
		opt := litestream.NewRestoreOptions()
		// Set the output path to the target directory plus the relative path of the database
		opt.OutputPath = filepath.Join(targetDir, strings.TrimPrefix(dbPath, clonedBucketRoot))
		syncInterval := litestream.DefaultSyncInterval
		r, err := NewReplicaFromConfig(&ReplicaConfig{
			Path:         dbPath,
			SyncInterval: &syncInterval,
		}, nil)
		if err != nil {
			replicaInitErrors = append(replicaInitErrors, fmt.Errorf("failed to create replica for %s: %w", dbPath, err))
			continue
		}
		opt.Generation, _, err = r.CalcRestoreTarget(ctx, opt)
		if err != nil {
			replicaInitErrors = append(replicaInitErrors, fmt.Errorf("failed to calculate restore target for %s: %w", dbPath, err))
			continue
		}
		replicas = append(replicas, replicaOpt{
			replica: r,
			opt:     &opt,
			source:  dbPath,
		})
	}

	taskCh := make(chan replicaOpt, len(replicas))
	defer close(taskCh)
	errorCh := make(chan error, len(replicas))

	numRoutines := cctx.Int("concurrency")
	for i := 0; i < numRoutines; i++ {
		go func() {
			for task := range taskCh {
				log := slog.With("dest_path", task.opt.OutputPath, "generation", task.opt.Generation, "source_path", task.source)
				log.Warn("restoring replica")
				if err := task.replica.Restore(ctx, *task.opt); err != nil {
					errorCh <- fmt.Errorf("failed to restore replica %s: %w", task.opt.OutputPath, err)
					log.Error("failed to restore replica", "error", err)
					continue
				}
				log.Warn("restored replica")
				errorCh <- nil
			}
		}()
	}

	// Send tasks to the worker goroutines
	for _, item := range replicas {
		taskCh <- item
	}

	replicaRestoreErrors := []error{}
	for range replicas {
		err := <-errorCh
		if err != nil {
			replicaRestoreErrors = append(replicaRestoreErrors, err)
		}
	}

	slog.Warn("restore complete",
		"source_dbs_discovered", len(dbPaths),
		"successfully_restored_replicas", len(replicas)-len(replicaRestoreErrors),
		"unsuccessfully_initialized_replicas", len(replicaInitErrors),
		"unsuccessfully_restored_replicas", len(replicaRestoreErrors),
		"duration", time.Since(start).String(),
		"replica_init_errors", replicaInitErrors,
		"replica_restore_errors", replicaRestoreErrors,
	)

	return nil
}

// Replicate loads all databases specified in the configuration.
func Replicate(cctx *cli.Context) (err error) {
	logLvl := new(slog.LevelVar)
	logLvl.UnmarshalText([]byte(cctx.String("log-level")))
	slog.SetDefault(slog.New(slog.NewJSONHandler(os.Stderr, &slog.HandlerOptions{
		Level: logLvl,
	})))

	// Display version information.
	slog.Warn("literiver starting up",
		"target_directories", cctx.StringSlice("dir"),
		"replica_root", cctx.String("replica-root"),
		"db_ttl", cctx.Duration("db-ttl").String(),
		"sync_interval", cctx.Duration("sync-interval").String(),
		"metrics_addr", cctx.String("addr"),
		"log_level", cctx.String("log-level"),
	)

	dirs := []string{}
	for _, dir := range cctx.StringSlice("dir") {
		if dir, err = filepath.Abs(dir); err != nil {
			return fmt.Errorf("failed to resolve directory path (%s): %w", dir, err)
		}
		dirs = append(dirs, dir)
	}

	// Load configuration.
	conf := Config{
		Dirs:         dirs,
		ReplicaRoot:  cctx.String("replica-root"),
		Addr:         cctx.String("addr"),
		DBTTL:        cctx.Duration("db-ttl"),
		SyncInterval: cctx.Duration("sync-interval"),
		MaxActiveDBs: cctx.Int("max-active-dbs"),
	}

	// Initialize replicator
	r := NewReplicator(conf)

	// Discover databases.
	if len(r.Config.Dirs) == 0 {
		slog.Error("no directories specified in configuration")
		return nil
	}

	// Watch directories for changes in a separate goroutine.
	shutdown := make(chan struct{})
	go func() {
		if err := r.watchDirs(r.Config.Dirs, func(path string) {
			if err := r.processDBUpdate(path); err != nil {
				slog.Error("failed to process DB Update", "error", err)
			}
		}, shutdown); err != nil {
			slog.Error("failed to watch directories", "error", err)
		}
	}()

	// Serve metrics over HTTP if enabled.
	if r.Config.Addr != "" {
		hostport := r.Config.Addr
		if host, port, _ := net.SplitHostPort(r.Config.Addr); port == "" {
			return fmt.Errorf("must specify port for bind address: %q", r.Config.Addr)
		} else if host == "" {
			hostport = net.JoinHostPort("localhost", port)
		}

		// Filter out metrics from litestream due to high cardinality (4 series per DB)
		metricHandler := promhttp.HandlerFor(
			NewFilteredGatherer(prometheus.DefaultGatherer, "litestream_"),
			promhttp.HandlerOpts{},
		)

		slog.Warn("serving metrics on", "url", fmt.Sprintf("http://%s/metrics", hostport))
		go func() {
			http.Handle("/metrics", metricHandler)
			if err := http.ListenAndServe(r.Config.Addr, nil); err != nil {
				slog.Error("cannot start metrics server", "error", err)
			}
		}()
	}

	// Trap SIGINT to trigger a shutdown.
	signals := make(chan os.Signal, 1)
	signal.Notify(signals, syscall.SIGINT, syscall.SIGTERM)

	select {
	case <-signals:
		slog.Warn("shutting down on signal")
	}

	slog.Warn("shutting down, waiting for workers to clean up...")

	// Close the watcher
	close(shutdown)

	// Sync and close all active DBs
	r.Close()

	slog.Warn("all workers shutdown")
	return nil
}
