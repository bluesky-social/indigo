package main

import (
	"context"
	"log/slog"
	_ "net/http/pprof"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/bluesky-social/indigo/events"
	"github.com/bluesky-social/indigo/splitter"

	"github.com/carlmjohnson/versioninfo"
	_ "github.com/joho/godotenv/autoload"
	"github.com/urfave/cli/v2"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp"
	"go.opentelemetry.io/otel/sdk/resource"
	tracesdk "go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.4.0"
	_ "go.uber.org/automaxprocs"
)

var log = slog.Default().With("system", "rainbow")

func init() {
	// control log level using, eg, GOLOG_LOG_LEVEL=debug
	//logging.SetAllLoggers(logging.LevelDebug)
}

func main() {
	run(os.Args)
}

func run(args []string) {
	app := cli.App{
		Name:    "rainbow",
		Usage:   "atproto firehose fan-out daemon",
		Version: versioninfo.Short(),
	}

	app.Flags = []cli.Flag{
		// TODO: unimplemented, always assumes https:// and wss://
		//&cli.BoolFlag{
		//	Name:    "crawl-insecure-ws",
		//	Usage:   "when connecting to PDS instances, use ws:// instead of wss://",
		//	EnvVars: []string{"RAINBOW_INSECURE_CRAWL"},
		//},
		&cli.StringFlag{
			Name:    "splitter-host",
			Value:   "bsky.network",
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
			Name:    "next-crawler",
			Usage:   "forward POST requestCrawl to this url, should be machine root url and not xrpc/requestCrawl, comma separated list",
			EnvVars: []string{"RELAY_NEXT_CRAWLER"},
		},
	}

	// TODO: slog.SetDefault and set module `var log *slog.Logger` based on flags and env

	app.Action = Splitter
	err := app.Run(os.Args)
	if err != nil {
		log.Error(err.Error())
		os.Exit(1)
	}
}

func Splitter(cctx *cli.Context) error {
	// Trap SIGINT to trigger a shutdown.
	signals := make(chan os.Signal, 1)
	signal.Notify(signals, syscall.SIGINT, syscall.SIGTERM)

	// Enable OTLP HTTP exporter
	// For relevant environment variables:
	// https://pkg.go.dev/go.opentelemetry.io/otel/exporters/otlp/otlptrace#readme-environment-variables
	// At a minimum, you need to set
	// OTEL_EXPORTER_OTLP_ENDPOINT=http://localhost:4318
	if ep := os.Getenv("OTEL_EXPORTER_OTLP_ENDPOINT"); ep != "" {
		log.Info("setting up trace exporter", "endpoint", ep)
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()

		exp, err := otlptracehttp.New(ctx)
		if err != nil {
			log.Error("failed to create trace exporter", "error", err)
			os.Exit(1)
		}
		defer func() {
			ctx, cancel := context.WithTimeout(context.Background(), time.Second)
			defer cancel()
			if err := exp.Shutdown(ctx); err != nil {
				log.Error("failed to shutdown trace exporter", "error", err)
			}
		}()

		tp := tracesdk.NewTracerProvider(
			tracesdk.WithBatcher(exp),
			tracesdk.WithResource(resource.NewWithAttributes(
				semconv.SchemaURL,
				semconv.ServiceNameKey.String("splitter"),
				attribute.String("env", os.Getenv("ENVIRONMENT")),         // DataDog
				attribute.String("environment", os.Getenv("ENVIRONMENT")), // Others
				attribute.Int64("ID", 1),
			)),
		)
		otel.SetTracerProvider(tp)
	}

	persistPath := cctx.String("persist-db")
	upstreamHost := cctx.String("splitter-host")
	nextCrawlers := cctx.StringSlice("next-crawler")

	var spl *splitter.Splitter
	var err error
	if persistPath != "" {
		log.Info("building splitter with storage at", "path", persistPath)
		ppopts := events.PebblePersistOptions{
			DbPath:          persistPath,
			PersistDuration: time.Duration(float64(time.Hour) * cctx.Float64("persist-hours")),
			GCPeriod:        5 * time.Minute,
			MaxBytes:        uint64(cctx.Int64("persist-bytes")),
		}
		conf := splitter.SplitterConfig{
			UpstreamHost:  upstreamHost,
			CursorFile:    cctx.String("cursor-file"),
			PebbleOptions: &ppopts,
		}
		spl, err = splitter.NewSplitter(conf, nextCrawlers)
	} else {
		log.Info("building in-memory splitter")
		conf := splitter.SplitterConfig{
			UpstreamHost: upstreamHost,
			CursorFile:   cctx.String("cursor-file"),
		}
		spl, err = splitter.NewSplitter(conf, nextCrawlers)
	}
	if err != nil {
		log.Error("failed to create splitter", "path", persistPath, "error", err)
		os.Exit(1)
		return err
	}

	// set up metrics endpoint
	go func() {
		if err := spl.StartMetrics(cctx.String("metrics-listen")); err != nil {
			log.Error("failed to start metrics endpoint", "err", err)
			os.Exit(1)
		}
	}()

	runErr := make(chan error, 1)

	go func() {
		err := spl.Start(cctx.String("api-listen"))
		runErr <- err
	}()

	log.Info("startup complete")
	select {
	case <-signals:
		log.Info("received shutdown signal")
		if err := spl.Shutdown(); err != nil {
			log.Error("error during Splitter shutdown", "err", err)
		}
	case err := <-runErr:
		if err != nil {
			log.Error("error during Splitter startup", "err", err)
		}
		log.Info("shutting down")
		if err := spl.Shutdown(); err != nil {
			log.Error("error during Splitter shutdown", "err", err)
		}
	}

	log.Info("shutdown complete")

	return nil
}
