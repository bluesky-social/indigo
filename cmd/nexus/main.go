package main

import (
	"context"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"

	_ "github.com/joho/godotenv/autoload"

	"github.com/carlmjohnson/versioninfo"
	"github.com/urfave/cli/v2"
)

func main() {
	if err := run(os.Args); err != nil {
		slog.Error("exiting process", "error", err)
		os.Exit(-1)
	}
}

func run(args []string) error {
	app := cli.App{
		Name:    "nexus",
		Usage:   "atproto sync service",
		Version: versioninfo.Short(),
	}

	app.Flags = []cli.Flag{
		&cli.StringFlag{
			Name:    "env",
			Usage:   "environment name for observability",
			Value:   "dev",
			EnvVars: []string{"NEXUS_ENV"},
		},
		&cli.BoolFlag{
			Name: "enable-jaeger-tracing",
		},
		&cli.BoolFlag{
			Name: "enable-otel-tracing",
		},
		&cli.StringFlag{
			Name:    "otel-exporter-otlp-endpoint",
			EnvVars: []string{"OTEL_EXPORTER_OTLP_ENDPOINT"},
		},
		&cli.StringFlag{
			Name:    "database-url",
			Usage:   "database connection string (sqlite://path or postgres://...)",
			Value:   "sqlite://./nexus.db",
			EnvVars: []string{"NEXUS_DATABASE_URL"},
		},
		&cli.IntFlag{
			Name:    "db-max-conns",
			Usage:   "maximum number of database connections",
			Value:   32,
			EnvVars: []string{"NEXUS_DB_MAX_CONNS"},
		},
		&cli.StringFlag{
			Name:    "relay-url",
			Usage:   "AT Protocol relay URL",
			Value:   "https://relay1.us-east.bsky.network",
			EnvVars: []string{"NEXUS_RELAY_URL"},
		},
		&cli.StringFlag{
			Name:    "bind",
			Usage:   "address and port to listen on for HTTP APIs",
			Value:   ":8080",
			EnvVars: []string{"NEXUS_BIND"},
		},
		&cli.IntFlag{
			Name:    "firehose-parallelism",
			Usage:   "number of parallel firehose event processors",
			Value:   10,
			EnvVars: []string{"NEXUS_FIREHOSE_PARALLELISM"},
		},
		&cli.IntFlag{
			Name:    "resync-parallelism",
			Usage:   "number of parallel resync workers",
			Value:   5,
			EnvVars: []string{"NEXUS_RESYNC_PARALLELISM"},
		},
		&cli.IntFlag{
			Name:    "outbox-parallelism",
			Usage:   "number of parallel outbox event loaders",
			Value:   1,
			EnvVars: []string{"NEXUS_OUTBOX_PARALLELISM"},
		},
		&cli.DurationFlag{
			Name:    "cursor-save-interval",
			Usage:   "how often to save firehose cursor",
			Value:   0,
			EnvVars: []string{"NEXUS_CURSOR_SAVE_INTERVAL"},
		},
		&cli.DurationFlag{
			Name:    "repo-fetch-timeout",
			Usage:   "timeout when fetching repo CARs from PDS (e.g. 180s for slow hosts)",
			Value:   30 * time.Second,
			EnvVars: []string{"NEXUS_REPO_FETCH_TIMEOUT"},
		},
		&cli.BoolFlag{
			Name:    "full-network-mode",
			Usage:   "enumerate and sync all repos on the network",
			EnvVars: []string{"NEXUS_FULL_NETWORK_MODE"},
		},
		&cli.StringFlag{
			Name:    "signal-collection",
			Usage:   "enumerate repos by collection (exact NSID)",
			EnvVars: []string{"NEXUS_SIGNAL_COLLECTION"},
		},
		&cli.BoolFlag{
			Name:    "disable-acks",
			Usage:   "disable client acknowledgments (fire-and-forget mode)",
			EnvVars: []string{"NEXUS_DISABLE_ACKS"},
		},
		&cli.StringFlag{
			Name:    "webhook-url",
			Usage:   "webhook URL for event delivery (instead of WebSocket)",
			EnvVars: []string{"NEXUS_WEBHOOK_URL"},
		},
		&cli.StringSliceFlag{
			Name:    "collection-filters",
			Usage:   "filter output records by collection (supports wildcards)",
			EnvVars: []string{"NEXUS_COLLECTION_FILTERS"},
		},
		&cli.StringFlag{
			Name:    "log-level",
			Usage:   "log verbosity level (debug, info, warn, error)",
			Value:   "info",
			EnvVars: []string{"NEXUS_LOG_LEVEL", "LOG_LEVEL"},
		},
		&cli.BoolFlag{
			Name:    "outbox-only",
			Usage:   "run in outbox-only mode (no firehose, resync, or enumeration)",
			EnvVars: []string{"NEXUS_OUTBOX_ONLY"},
		},
	}

	app.Action = runNexus

	return app.Run(args)
}

func runNexus(cctx *cli.Context) error {
	if err := setupOTEL(cctx); err != nil {
		return err
	}

	ctx, cancel := context.WithCancel(cctx.Context)
	logger := configLogger(cctx, os.Stdout)
	slog.SetDefault(logger)

	signals := make(chan os.Signal, 1)
	signal.Notify(signals, syscall.SIGINT, syscall.SIGTERM)

	config := NexusConfig{
		DatabaseURL:                cctx.String("database-url"),
		DBMaxConns:                 cctx.Int("db-max-conns"),
		RelayUrl:                   cctx.String("relay-url"),
		FirehoseParallelism:        cctx.Int("firehose-parallelism"),
		ResyncParallelism:          cctx.Int("resync-parallelism"),
		OutboxParallelism:          cctx.Int("outbox-parallelism"),
		FirehoseCursorSaveInterval: cctx.Duration("cursor-save-interval"),
		RepoFetchTimeout:           cctx.Duration("repo-fetch-timeout"),
		FullNetworkMode:            cctx.Bool("full-network-mode"),
		SignalCollection:           cctx.String("signal-collection"),
		DisableAcks:                cctx.Bool("disable-acks"),
		WebhookURL:                 cctx.String("webhook-url"),
		CollectionFilters:          cctx.StringSlice("collection-filters"),
		OutboxOnly:                 cctx.Bool("outbox-only"),
	}

	logger.Info("creating nexus service")
	nexus, err := NewNexus(config)
	if err != nil {
		return err
	}

	if !config.OutboxOnly {
		if config.SignalCollection != "" {
			go func() {
				if err := nexus.Crawler.EnumerateNetworkByCollection(ctx, config.SignalCollection); err != nil {
					logger.Error("collection enumeration failed", "error", err, "collection", config.SignalCollection)
				}
			}()
		} else if config.FullNetworkMode {
			go func() {
				if err := nexus.Crawler.EnumerateNetwork(ctx); err != nil {
					logger.Error("network enumeration failed", "error", err)
				}
			}()
		}
	}

	svcErr := make(chan error, 1)

	if !config.OutboxOnly {
		go func() {
			logger.Info("starting firehose consumer")
			if err := nexus.FirehoseConsumer.Run(ctx); err != nil {
				svcErr <- err
			}
		}()
	}

	go nexus.Run(ctx)

	go func() {
		logger.Info("starting HTTP server", "addr", cctx.String("bind"))
		if err := nexus.Server.Start(cctx.String("bind")); err != nil {
			svcErr <- err
		}
	}()

	logger.Info("startup complete")
	select {
	case <-signals:
		logger.Info("received shutdown signal")
	case err := <-svcErr:
		if err != nil {
			logger.Error("service error", "error", err)
		}
	}

	logger.Info("shutting down")
	cancel()

	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer shutdownCancel()

	if err := nexus.Server.Shutdown(shutdownCtx); err != nil {
		logger.Error("error during shutdown", "error", err)
		return err
	}

	if err := nexus.CloseDb(shutdownCtx); err != nil {
		return err
	}

	logger.Info("shutdown complete")
	return nil
}

func configLogger(cctx *cli.Context, writer *os.File) *slog.Logger {
	var level slog.Level
	switch cctx.String("log-level") {
	case "debug":
		level = slog.LevelDebug
	case "info":
		level = slog.LevelInfo
	case "warn":
		level = slog.LevelWarn
	case "error":
		level = slog.LevelError
	default:
		level = slog.LevelInfo
	}

	logger := slog.New(slog.NewJSONHandler(writer, &slog.HandlerOptions{
		Level: level,
	}))

	return logger
}
