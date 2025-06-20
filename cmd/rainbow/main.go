package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"

	_ "github.com/joho/godotenv/autoload"
	_ "go.uber.org/automaxprocs"
	_ "net/http/pprof"

	"github.com/gander-social/gander-indigo-sovereign/events/pebblepersist"
	"github.com/gander-social/gander-indigo-sovereign/splitter"
	"github.com/gander-social/gander-indigo-sovereign/util/svcutil"

	"github.com/carlmjohnson/versioninfo"
	"github.com/urfave/cli/v2"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp"
	"go.opentelemetry.io/otel/sdk/resource"
	tracesdk "go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.4.0"
)

func main() {
	if err := run(os.Args); err != nil {
		slog.Error("exiting", "err", err)
		os.Exit(1)
	}
}

func run(args []string) error {

	app := cli.App{
		Name:    "rainbow",
		Usage:   "atproto firehose fan-out daemon",
		Version: versioninfo.Short(),
		Action:  runSplitter,
	}

	app.Flags = []cli.Flag{
		&cli.StringFlag{
			Name:    "log-level",
			Usage:   "log verbosity level (eg: warn, info, debug)",
			EnvVars: []string{"RAINBOW_LOG_LEVEL", "GO_LOG_LEVEL", "LOG_LEVEL"},
		},
		&cli.StringFlag{
			Name:    "upstream-host",
			Value:   "http://localhost:2470",
			Usage:   "URL (schema and hostname, no path) of the upstream host (eg, relay)",
			EnvVars: []string{"ATP_RELAY_HOST", "RAINBOW_RELAY_HOST"},
		},
		&cli.StringFlag{
			Name:    "persist-db",
			Value:   "./rainbow.db",
			Usage:   "path to persistence db",
			EnvVars: []string{"RAINBOW_DB_PATH"},
		},
		&cli.StringFlag{
			Name:    "cursor-file",
			Value:   "./rainbow-cursor",
			Usage:   "write upstream cursor number to this file",
			EnvVars: []string{"RAINBOW_CURSOR_PATH"},
		},
		&cli.StringFlag{
			Name:    "api-listen",
			Value:   ":2480",
			EnvVars: []string{"RAINBOW_API_LISTEN"},
		},
		&cli.StringFlag{
			Name:    "metrics-listen",
			Value:   ":2481",
			EnvVars: []string{"RAINBOW_METRICS_LISTEN", "SPLITTER_METRICS_LISTEN"},
		},
		&cli.Float64Flag{
			Name:    "persist-hours",
			Value:   24 * 3,
			EnvVars: []string{"RAINBOW_PERSIST_HOURS", "SPLITTER_PERSIST_HOURS"},
			Usage:   "hours to buffer (float, may be fractional)",
		},
		&cli.Int64Flag{
			Name:    "persist-bytes",
			Value:   0,
			Usage:   "max bytes target for event cache, 0 to disable size target trimming",
			EnvVars: []string{"RAINBOW_PERSIST_BYTES", "SPLITTER_PERSIST_BYTES"},
		},
		&cli.StringSliceFlag{
			// TODO: better name for this argument
			Name:    "next-crawler",
			Usage:   "forward POST requestCrawl to these hosts (schema and host, no path) in addition to upstream-host. Comma-separated or multiple flags",
			EnvVars: []string{"RAINBOW_NEXT_CRAWLER", "RELAY_NEXT_CRAWLER"},
		},
		&cli.StringFlag{
			Name:    "collectiondir-host",
			Value:   "http://localhost:2510",
			Usage:   "host (schema and hostname, no path) of upstream collectiondir instance, for com.atproto.sync.listReposByCollection",
			EnvVars: []string{"RAINBOW_COLLECTIONDIR_HOST"},
		},
		&cli.StringFlag{
			Name:    "env",
			Usage:   "operating environment (eg, 'prod', 'test')",
			Value:   "dev",
			EnvVars: []string{"ENVIRONMENT"},
		},
		&cli.BoolFlag{
			Name:    "enable-otel-otlp",
			Usage:   "enables OTEL OTLP exporter endpoint",
			EnvVars: []string{"RAINBOW_ENABLE_OTEL_OTLP", "ENABLE_OTEL_OTLP"},
		},
		&cli.StringFlag{
			Name:    "otel-otlp-endpoint",
			Usage:   "OTEL traces export endpoint",
			Value:   "http://localhost:4318",
			EnvVars: []string{"OTEL_EXPORTER_OTLP_ENDPOINT"},
		},
	}

	return app.Run(args)
}

func runSplitter(cctx *cli.Context) error {
	// Trap SIGINT to trigger a shutdown.
	signals := make(chan os.Signal, 1)
	signal.Notify(signals, syscall.SIGINT, syscall.SIGTERM)

	logger := svcutil.ConfigLogger(cctx, os.Stdout).With("system", "rainbow")

	// Enable OTLP HTTP exporter
	// For relevant environment variables:
	// https://pkg.go.dev/go.opentelemetry.io/otel/exporters/otlp/otlptrace#readme-environment-variables
	if cctx.Bool("enable-otel-otlp") {
		ep := cctx.String("otel-otlp-endpoint")
		logger.Info("setting up trace exporter", "endpoint", ep)
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()

		exp, err := otlptracehttp.New(ctx)
		if err != nil {
			logger.Error("failed to create trace exporter", "error", err)
			os.Exit(1)
		}
		defer func() {
			ctx, cancel := context.WithTimeout(context.Background(), time.Second)
			defer cancel()
			if err := exp.Shutdown(ctx); err != nil {
				logger.Error("failed to shutdown trace exporter", "error", err)
			}
		}()

		env := cctx.String("env")
		tp := tracesdk.NewTracerProvider(
			tracesdk.WithBatcher(exp),
			tracesdk.WithResource(resource.NewWithAttributes(
				semconv.SchemaURL,
				semconv.ServiceNameKey.String("splitter"),
				attribute.String("env", env),         // DataDog
				attribute.String("environment", env), // Others
				attribute.Int64("ID", 1),
			)),
		)
		otel.SetTracerProvider(tp)
	}

	persistPath := cctx.String("persist-db")
	upstreamHost := cctx.String("upstream-host")
	collectionDirHost := cctx.String("collectiondir-host")
	nextCrawlers := cctx.StringSlice("next-crawler")

	var spl *splitter.Splitter
	var err error
	if persistPath != "" {
		logger.Info("building splitter with storage at", "path", persistPath)
		ppopts := pebblepersist.PebblePersistOptions{
			DbPath:          persistPath,
			PersistDuration: time.Duration(float64(time.Hour) * cctx.Float64("persist-hours")),
			GCPeriod:        5 * time.Minute,
			MaxBytes:        uint64(cctx.Int64("persist-bytes")),
		}
		conf := splitter.SplitterConfig{
			UpstreamHost:      upstreamHost,
			CollectionDirHost: collectionDirHost,
			CursorFile:        cctx.String("cursor-file"),
			PebbleOptions:     &ppopts,
			UserAgent:         fmt.Sprintf("rainbow/%s (atproto-relay)", versioninfo.Short()),
		}
		spl, err = splitter.NewSplitter(conf, nextCrawlers)
	} else {
		logger.Info("building in-memory splitter")
		conf := splitter.SplitterConfig{
			UpstreamHost:      upstreamHost,
			CollectionDirHost: collectionDirHost,
			CursorFile:        cctx.String("cursor-file"),
		}
		spl, err = splitter.NewSplitter(conf, nextCrawlers)
	}
	if err != nil {
		logger.Error("failed to create splitter", "path", persistPath, "error", err)
		os.Exit(1)
		return err
	}

	// set up metrics endpoint
	metricsListen := cctx.String("metrics-listen")
	go func() {
		if err := spl.StartMetrics(metricsListen); err != nil {
			logger.Error("failed to start metrics endpoint", "err", err)
			os.Exit(1)
		}
	}()

	runErr := make(chan error, 1)

	go func() {
		err := spl.StartAPI(cctx.String("api-listen"))
		runErr <- err
	}()

	logger.Info("startup complete")
	select {
	case <-signals:
		logger.Info("received shutdown signal")
		if err := spl.Shutdown(); err != nil {
			logger.Error("error during Splitter shutdown", "err", err)
		}
	case err := <-runErr:
		if err != nil {
			logger.Error("error during Splitter startup", "err", err)
		}
		logger.Info("shutting down")
		if err := spl.Shutdown(); err != nil {
			logger.Error("error during Splitter shutdown", "err", err)
		}
	}

	logger.Info("shutdown complete")

	return nil
}
