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
	"syscall"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/urfave/cli/v2"
)

func main() {
	app := &cli.App{
		Name:  "literiver",
		Usage: "Replicate SQLite databases in a directory to S3",
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
		Action: Run,
	}

	if err := app.Run(os.Args); err != nil {
		slog.Error("failed to run", "error", err)
	}

	slog.Info("literiver exiting")
}

// Run loads all databases specified in the configuration.
func Run(cctx *cli.Context) (err error) {
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

	// Trim expired DBs in a separate goroutine.
	go func() {
		for {
			if err := r.expireDBs(); err != nil {
				slog.Error("failed to expire DBs", "error", err)
			}
			time.Sleep(time.Second * 5)
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
