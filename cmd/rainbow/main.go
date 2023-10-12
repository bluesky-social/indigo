package main

import (
	"context"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/bluesky-social/indigo/splitter"
	"github.com/bluesky-social/indigo/util/version"
	_ "go.uber.org/automaxprocs"

	_ "net/http/pprof"

	_ "github.com/joho/godotenv/autoload"

	logging "github.com/ipfs/go-log"
	"github.com/urfave/cli/v2"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp"
	"go.opentelemetry.io/otel/sdk/resource"
	tracesdk "go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.4.0"
)

var log = logging.Logger("splitter")

func init() {
	// control log level using, eg, GOLOG_LOG_LEVEL=debug
	logging.SetAllLoggers(logging.LevelDebug)
}

func main() {
	run(os.Args)
}

func run(args []string) {
	app := cli.App{
		Name:    "splitter",
		Usage:   "firehose proxy",
		Version: version.Version,
	}

	app.Flags = []cli.Flag{
		&cli.BoolFlag{
			Name:  "crawl-insecure-ws",
			Usage: "when connecting to PDS instances, use ws:// instead of wss://",
		},
		&cli.StringFlag{
			Name:  "splitter-host",
			Value: "bsky.network",
		},
		&cli.StringFlag{
			Name:  "api-listen",
			Value: ":2480",
		},
		&cli.StringFlag{
			Name:    "metrics-listen",
			Value:   ":2481",
			EnvVars: []string{"SPLITTER_METRICS_LISTEN"},
		},
	}

	app.Action = Splitter
	err := app.Run(os.Args)
	if err != nil {
		log.Fatal(err)
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
		log.Infow("setting up trace exporter", "endpoint", ep)
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()

		exp, err := otlptracehttp.New(ctx)
		if err != nil {
			log.Fatalw("failed to create trace exporter", "error", err)
		}
		defer func() {
			ctx, cancel := context.WithTimeout(context.Background(), time.Second)
			defer cancel()
			if err := exp.Shutdown(ctx); err != nil {
				log.Errorw("failed to shutdown trace exporter", "error", err)
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

	spl := splitter.NewSplitter(cctx.String("splitter-host"))

	// set up metrics endpoint
	go func() {
		if err := spl.StartMetrics(cctx.String("metrics-listen")); err != nil {
			log.Fatalf("failed to start metrics endpoint: %s", err)
		}
	}()

	runErr := make(chan error, 1)

	go func() {
		err := spl.Start(cctx.String("api-listen"))
		runErr <- err
	}()

	log.Infow("startup complete")
	select {
	case <-signals:
		log.Info("received shutdown signal")
		if err := spl.Shutdown(); err != nil {
			log.Errorw("error during Splitter shutdown", "err", err)
		}
	case err := <-runErr:
		if err != nil {
			log.Errorw("error during Splitter startup", "err", err)
		}
		log.Info("shutting down")
		if err := spl.Shutdown(); err != nil {
			log.Errorw("error during Splitter shutdown", "err", err)
		}
	}

	log.Info("shutdown complete")

	return nil
}
