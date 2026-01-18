package main

import (
	"context"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/bluesky-social/indigo/internal/cask/server"
	"github.com/bluesky-social/indigo/pkg/metrics"

	"github.com/earthboundkid/versioninfo/v2"
	"github.com/urfave/cli/v3"
)

func main() {
	if err := run(os.Args); err != nil {
		slog.Error("exiting", "err", err)
		os.Exit(1)
	}
}

func run(args []string) error {
	app := &cli.Command{
		Name:    "cask",
		Usage:   "High-availability atproto firehose fan-out daemon",
		Version: versioninfo.Short(),
		Commands: []*cli.Command{
			{
				Name:   "run",
				Usage:  "Start the cask server",
				Action: runServer,
				Flags: []cli.Flag{
					&cli.StringFlag{
						Name:    "log-level",
						Usage:   "log verbosity level (debug, info, warn, error)",
						Value:   "info",
						Sources: cli.EnvVars("CASK_LOG_LEVEL", "LOG_LEVEL"),
					},
					&cli.StringFlag{
						Name:    "env",
						Usage:   "environment name for observability (dev, staging, prod)",
						Value:   "dev",
						Sources: cli.EnvVars("CASK_ENV", "ENVIRONMENT"),
					},
					&cli.StringFlag{
						Name:    "api-listen",
						Usage:   "address and port to listen on for HTTP APIs",
						Value:   ":2480",
						Sources: cli.EnvVars("CASK_API_LISTEN"),
					},
					&cli.StringFlag{
						Name:    "metrics-listen",
						Usage:   "address and port for metrics/pprof server",
						Value:   ":2481",
						Sources: cli.EnvVars("CASK_METRICS_LISTEN"),
					},
					&cli.DurationFlag{
						Name:    "shutdown-timeout",
						Usage:   "max time to wait for graceful shutdown",
						Value:   30 * time.Second,
						Sources: cli.EnvVars("CASK_SHUTDOWN_TIMEOUT"),
					},
					&cli.StringFlag{
						Name:    "fdb-cluster-file",
						Usage:   "path to FoundationDB cluster file",
						Value:   "/etc/foundationdb/fdb.cluster",
						Sources: cli.EnvVars("CASK_FDB_CLUSTER_FILE", "FDB_CLUSTER_FILE"),
					},
					&cli.StringFlag{
						Name:    "firehose-url",
						Usage:   "upstream ATProto firehose websocket URL (e.g., wss://bsky.network)",
						Value:   "wss://bsky.network",
						Sources: cli.EnvVars("CASK_FIREHOSE_URL"),
					},
				},
			},
		},
	}

	return app.Run(context.Background(), args)
}

func runServer(ctx context.Context, cmd *cli.Command) error {
	logger := configLogger(cmd.String("log-level"))
	slog.SetDefault(logger)

	logger = logger.With("system", "cask")

	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	signals := make(chan os.Signal, 1)
	signal.Notify(signals, syscall.SIGINT, syscall.SIGTERM)

	srv, err := server.New(ctx, server.Config{
		Logger:         logger,
		FDBClusterFile: cmd.String("fdb-cluster-file"),
		FirehoseURL:    cmd.String("firehose-url"),
	})
	if err != nil {
		return err
	}

	svcErr := make(chan error, 2)

	metricsAddr := cmd.String("metrics-listen")
	go func() {
		logger.Info("starting metrics server", "addr", metricsAddr)
		if err := metrics.RunServer(ctx, cancel, metricsAddr); err != nil {
			logger.Error("metrics server failed", "error", err)
			svcErr <- err
		}
	}()

	apiAddr := cmd.String("api-listen")
	go func() {
		logger.Info("starting API server", "addr", apiAddr)
		if err := srv.Start(ctx, apiAddr); err != nil && err != http.ErrServerClosed {
			logger.Error("API server failed", "error", err)
			svcErr <- err
		}
	}()

	logger.Info("startup complete")

	// Wait for shutdown signal or error
	select {
	case <-signals:
		logger.Info("received shutdown signal")
	case err := <-svcErr:
		if err != nil {
			logger.Error("error running cask server", "error", err)
		}
	}

	ctx, shutdownCancel := context.WithTimeout(context.Background(), cmd.Duration("shutdown-timeout"))
	defer shutdownCancel()

	logger.Info("shutting down")
	if err := srv.Shutdown(ctx); err != nil {
		logger.Error("error during shutdown", "error", err)
		return err
	}

	logger.Info("shutdown complete")
	return nil
}

func configLogger(level string) *slog.Logger {
	var logLevel slog.Level
	switch level {
	case "debug":
		logLevel = slog.LevelDebug
	case "info":
		logLevel = slog.LevelInfo
	case "warn":
		logLevel = slog.LevelWarn
	case "error":
		logLevel = slog.LevelError
	default:
		logLevel = slog.LevelInfo
	}

	return slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{
		Level: logLevel,
	}))
}
