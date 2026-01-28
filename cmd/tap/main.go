package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"net/http"
	_ "net/http/pprof"

	_ "github.com/joho/godotenv/autoload"
	"github.com/prometheus/client_golang/prometheus/promhttp"

	"github.com/earthboundkid/versioninfo/v2"
	"github.com/urfave/cli/v3"
)

func main() {
	if err := run(os.Args); err != nil {
		slog.Error("exiting process", "error", err)
		os.Exit(-1)
	}
}

func run(args []string) error {
	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()

	app := &cli.Command{
		Name:    "tap",
		Usage:   "atproto sync tool",
		Version: versioninfo.Short(),
		Commands: []*cli.Command{
			{
				Name:  "run",
				Usage: "start the tap server",
				Flags: []cli.Flag{
					&cli.StringFlag{
						Name:    "env",
						Usage:   "environment name for observability",
						Value:   "dev",
						Sources: cli.EnvVars("TAP_ENV"),
					},
					&cli.StringFlag{
						Name:    "db-url",
						Usage:   "database connection string (sqlite://path or postgres://...)",
						Value:   "sqlite://./tap.db",
						Sources: cli.EnvVars("TAP_DATABASE_URL"),
					},
					&cli.IntFlag{
						Name:    "max-db-conn",
						Usage:   "maximum number of database connections",
						Value:   32,
						Sources: cli.EnvVars("TAP_MAX_DB_CONNS"),
					},
					&cli.StringFlag{
						Name:    "bind",
						Usage:   "address and port to listen on for HTTP APIs",
						Value:   ":2480",
						Sources: cli.EnvVars("TAP_BIND"),
					},
					&cli.StringFlag{
						Name:    "plc-url",
						Usage:   "PLC registry HTTP/HTTPS url",
						Value:   "https://plc.directory",
						Sources: cli.EnvVars("TAP_PLC_URL", "ATP_PLC_HOST"),
					},
					&cli.StringFlag{
						Name:    "relay-url",
						Usage:   "AT Protocol relay HTTP/HTTPS url",
						Value:   "https://relay1.us-east.bsky.network",
						Sources: cli.EnvVars("TAP_RELAY_URL"),
					},
					&cli.IntFlag{
						Name:    "firehose-parallelism",
						Usage:   "number of parallel firehose event processors",
						Value:   10,
						Sources: cli.EnvVars("TAP_FIREHOSE_PARALLELISM"),
					},
					&cli.IntFlag{
						Name:    "resync-parallelism",
						Usage:   "number of parallel resync workers",
						Value:   5,
						Sources: cli.EnvVars("TAP_RESYNC_PARALLELISM"),
					},
					&cli.IntFlag{
						Name:    "outbox-parallelism",
						Usage:   "number of parallel outbox workers",
						Value:   1,
						Sources: cli.EnvVars("TAP_OUTBOX_PARALLELISM"),
					},
					&cli.DurationFlag{
						Name:    "cursor-save-interval",
						Usage:   "how often to save firehose cursor",
						Value:   1 * time.Second,
						Sources: cli.EnvVars("TAP_CURSOR_SAVE_INTERVAL"),
					},
					&cli.DurationFlag{
						Name:    "repo-fetch-timeout",
						Usage:   "timeout when fetching repo CARs from PDS (e.g. 180s)",
						Value:   300 * time.Second,
						Sources: cli.EnvVars("TAP_REPO_FETCH_TIMEOUT"),
					},
					&cli.IntFlag{
						Name:    "ident-cache-size",
						Usage:   "size of in-process identity cache",
						Value:   2_000_000,
						Sources: cli.EnvVars("RELAY_IDENT_CACHE_SIZE"),
					},
					&cli.IntFlag{
						Name:    "outbox-capacity",
						Usage:   "rough size of outbox before back pressure is applied",
						Value:   100_000,
						Sources: cli.EnvVars("TAP_OUTBOX_CAPACITY"),
					},
					&cli.BoolFlag{
						Name:    "full-network",
						Usage:   "enumerate and sync all repos on the network",
						Sources: cli.EnvVars("TAP_FULL_NETWORK"),
					},
					&cli.StringFlag{
						Name:    "signal-collection",
						Usage:   "enumerate repos by collection (exact NSID)",
						Sources: cli.EnvVars("TAP_SIGNAL_COLLECTION"),
					},
					&cli.BoolFlag{
						Name:    "disable-acks",
						Usage:   "disable client acknowledgments (fire-and-forget mode)",
						Sources: cli.EnvVars("TAP_DISABLE_ACKS"),
					},
					&cli.StringFlag{
						Name:    "webhook-url",
						Usage:   "webhook URL for event delivery (instead of WebSocket)",
						Sources: cli.EnvVars("TAP_WEBHOOK_URL"),
					},
					&cli.StringSliceFlag{
						Name:    "collection-filters",
						Usage:   "filter output records by collection (supports wildcards)",
						Sources: cli.EnvVars("TAP_COLLECTION_FILTERS"),
					},
					&cli.BoolFlag{
						Name:    "outbox-only",
						Usage:   "run in outbox-only mode (no firehose, resync, or enumeration)",
						Sources: cli.EnvVars("TAP_OUTBOX_ONLY"),
					},
					&cli.StringFlag{
						Name:    "admin-password",
						Usage:   "Basic auth admin password required for all requests (if set)",
						Sources: cli.EnvVars("TAP_ADMIN_PASSWORD"),
					},
					&cli.DurationFlag{
						Name:    "retry-timeout",
						Usage:   "timeout before retrying unacked events",
						Value:   60 * time.Second,
						Sources: cli.EnvVars("TAP_RETRY_TIMEOUT"),
					},
					&cli.StringFlag{
						Name:    "log-level",
						Usage:   "log verbosity level (debug, info, warn, error)",
						Value:   "info",
						Sources: cli.EnvVars("TAP_LOG_LEVEL", "LOG_LEVEL"),
					},
					&cli.StringFlag{
						Name:    "metrics-listen",
						Usage:   "address for metrics/pprof server (disabled if empty)",
						Sources: cli.EnvVars("TAP_METRICS_LISTEN"),
					},
				},
				Action: runTap,
			},
		},
	}

	return app.Run(ctx, args)
}

func runTap(ctx context.Context, cmd *cli.Command) error {
	logger := configLogger(cmd, os.Stdout)
	slog.SetDefault(logger)

	// fail early if relay url is not http/https
	relayUrl := cmd.String("relay-url")
	if !strings.HasPrefix(relayUrl, "http://") && !strings.HasPrefix(relayUrl, "https://") {
		return fmt.Errorf("relay-url must start with http:// or https://")
	}

	// fail early if plc url is not http/https
	plcUrl := cmd.String("plc-url")
	if !strings.HasPrefix(plcUrl, "http://") && !strings.HasPrefix(plcUrl, "https://") {
		return fmt.Errorf("plc-url must start with http:// or https://")
	}

	config := TapConfig{
		DatabaseURL:                cmd.String("db-url"),
		DBMaxConns:                 int(cmd.Int("max-db-conn")),
		PLCURL:                     plcUrl,
		RelayUrl:                   relayUrl,
		FirehoseParallelism:        int(cmd.Int("firehose-parallelism")),
		ResyncParallelism:          int(cmd.Int("resync-parallelism")),
		OutboxParallelism:          int(cmd.Int("outbox-parallelism")),
		FirehoseCursorSaveInterval: cmd.Duration("cursor-save-interval"),
		RepoFetchTimeout:           cmd.Duration("repo-fetch-timeout"),
		IdentityCacheSize:          int(cmd.Int("ident-cache-size")),
		EventCacheSize:             int(cmd.Int("outbox-capacity")),
		FullNetworkMode:            cmd.Bool("full-network"),
		SignalCollection:           cmd.String("signal-collection"),
		DisableAcks:                cmd.Bool("disable-acks"),
		WebhookURL:                 cmd.String("webhook-url"),
		CollectionFilters:          cmd.StringSlice("collection-filters"),
		OutboxOnly:                 cmd.Bool("outbox-only"),
		AdminPassword:              cmd.String("admin-password"),
		RetryTimeout:               cmd.Duration("retry-timeout"),
	}

	logger.Info("creating tap service")
	tap, err := NewTap(config)
	if err != nil {
		return err
	}

	if !config.OutboxOnly {
		go tap.Crawler.Run(ctx)
	}

	svcErr := make(chan error, 1)

	if !config.OutboxOnly {
		go func() {
			logger.Info("starting firehose consumer")
			if err := tap.Firehose.Run(ctx); err != nil {
				svcErr <- err
			}
		}()
	}

	go tap.Run(ctx)

	go func() {
		logger.Info("starting HTTP server", "addr", cmd.String("bind"))
		if err := tap.Server.Start(cmd.String("bind")); err != nil {
			svcErr <- err
		}
	}()

	if metricsAddr := cmd.String("metrics-listen"); metricsAddr != "" {
		go func() {
			logger.Info("starting metrics server", "addr", metricsAddr)
			// RunMetrics starts the metrics and pprof server on a separate port.
			http.Handle("/metrics", promhttp.Handler())
			if err := http.ListenAndServe(metricsAddr, nil); err != nil {
				logger.Error("metrics server failed", "error", err)
			}
		}()
	}

	logger.Info("startup complete")
	select {
	case <-ctx.Done():
		logger.Info("received shutdown signal", "reason", ctx.Err())
	case err := <-svcErr:
		if err != nil {
			logger.Error("service error", "error", err)
		}
	}

	logger.Info("shutting down")

	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer shutdownCancel()

	if err := tap.Server.Shutdown(shutdownCtx); err != nil {
		logger.Error("error during shutdown", "error", err)
		return err
	}

	if err := tap.CloseDb(shutdownCtx); err != nil {
		return err
	}

	logger.Info("shutdown complete")
	return nil
}

func configLogger(cmd *cli.Command, writer *os.File) *slog.Logger {
	var level slog.Level
	switch cmd.String("log-level") {
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
