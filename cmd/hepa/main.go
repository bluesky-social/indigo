package main

import (
	"context"
	"fmt"
	"log"
	"log/slog"
	"net/http"
	"os"
	"time"

	"github.com/bluesky-social/indigo/atproto/identity"
	"github.com/bluesky-social/indigo/util/cliutil"

	"github.com/carlmjohnson/versioninfo"
	_ "github.com/joho/godotenv/autoload"
	cli "github.com/urfave/cli/v2"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp"
	"go.opentelemetry.io/otel/sdk/resource"
	tracesdk "go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.4.0"
	"golang.org/x/time/rate"
)

func main() {
	if err := run(os.Args); err != nil {
		slog.Error("exiting", "err", err)
		os.Exit(-1)
	}
}

func run(args []string) error {

	app := cli.App{
		Name:    "hepa",
		Usage:   "automod daemon (cleans the atmosphere)",
		Version: versioninfo.Short(),
	}

	app.Flags = []cli.Flag{
		&cli.StringFlag{
			Name:    "atp-bgs-host",
			Usage:   "hostname and port of BGS to subscribe to",
			Value:   "wss://bsky.network",
			EnvVars: []string{"ATP_BGS_HOST"},
		},
		&cli.StringFlag{
			Name:    "atp-plc-host",
			Usage:   "method, hostname, and port of PLC registry",
			Value:   "https://plc.directory",
			EnvVars: []string{"ATP_PLC_HOST"},
		},
		&cli.IntFlag{
			Name:    "max-metadb-connections",
			EnvVars: []string{"MAX_METADB_CONNECTIONS"},
			Value:   40,
		},
	}

	app.Commands = []*cli.Command{
		runCmd,
	}

	return app.Run(args)
}

var runCmd = &cli.Command{
	Name:  "run",
	Usage: "run the service",
	Flags: []cli.Flag{
		&cli.StringFlag{
			Name:    "database-url",
			Value:   "sqlite://data/hepa/automod.db",
			EnvVars: []string{"DATABASE_URL"},
		},
		&cli.BoolFlag{
			Name:    "readonly",
			EnvVars: []string{"HEPA_READONLY", "READONLY"},
		},
		&cli.StringFlag{
			Name:    "bind",
			Usage:   "IP or address, and port, to listen on for HTTP APIs",
			Value:   ":3999",
			EnvVars: []string{"HEPA_BIND"},
		},
		&cli.StringFlag{
			Name:    "metrics-listen",
			Usage:   "IP or address, and port, to listen on for metrics APIs",
			Value:   ":3998",
			EnvVars: []string{"HEPA_METRICS_LISTEN"},
		},
		&cli.IntFlag{
			Name:    "bgs-sync-rate-limit",
			Usage:   "max repo sync (checkout) requests per second to upstream (BGS)",
			Value:   8,
			EnvVars: []string{"HEPA_BGS_SYNC_RATE_LIMIT"},
		},
		&cli.IntFlag{
			Name:    "plc-rate-limit",
			Usage:   "max number of requests per second to PLC registry",
			Value:   100,
			EnvVars: []string{"HEPA_PLC_RATE_LIMIT"},
		},
	},
	Action: func(cctx *cli.Context) error {
		ctx := context.Background()
		logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{
			Level: slog.LevelInfo,
		}))
		slog.SetDefault(logger)

		// Enable OTLP HTTP exporter
		// For relevant environment variables:
		// https://pkg.go.dev/go.opentelemetry.io/otel/exporters/otlp/otlptrace#readme-environment-variables
		// At a minimum, you need to set
		// OTEL_EXPORTER_OTLP_ENDPOINT=http://localhost:4318
		if ep := os.Getenv("OTEL_EXPORTER_OTLP_ENDPOINT"); ep != "" {
			slog.Info("setting up trace exporter", "endpoint", ep)
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()

			exp, err := otlptracehttp.New(ctx)
			if err != nil {
				log.Fatal("failed to create trace exporter", "error", err)
			}
			defer func() {
				ctx, cancel := context.WithTimeout(context.Background(), time.Second)
				defer cancel()
				if err := exp.Shutdown(ctx); err != nil {
					slog.Error("failed to shutdown trace exporter", "error", err)
				}
			}()

			tp := tracesdk.NewTracerProvider(
				tracesdk.WithBatcher(exp),
				tracesdk.WithResource(resource.NewWithAttributes(
					semconv.SchemaURL,
					semconv.ServiceNameKey.String("hepa"),
					attribute.String("env", os.Getenv("ENVIRONMENT")),         // DataDog
					attribute.String("environment", os.Getenv("ENVIRONMENT")), // Others
					attribute.Int64("ID", 1),
				)),
			)
			otel.SetTracerProvider(tp)
		}

		db, err := cliutil.SetupDatabase(cctx.String("database-url"), cctx.Int("max-metadb-connections"))
		if err != nil {
			return err
		}

		// TODO: replace this with "bingo" resolver?
		base := identity.BaseDirectory{
			PLCURL: cctx.String("atp-plc-host"),
			HTTPClient: http.Client{
				Timeout: time.Second * 15,
			},
			PLCLimiter:            rate.NewLimiter(rate.Limit(cctx.Int("plc-rate-limit")), 1),
			TryAuthoritativeDNS:   true,
			SkipDNSDomainSuffixes: []string{".bsky.social"},
		}
		dir := identity.NewCacheDirectory(&base, 1_500_000, time.Hour*24, time.Minute*2)

		srv, err := NewServer(
			db,
			&dir,
			Config{
				BGSHost:          cctx.String("atp-bgs-host"),
				Logger:           logger,
				BGSSyncRateLimit: cctx.Int("bgs-sync-rate-limit"),
			},
		)
		if err != nil {
			return err
		}

		go func() {
			if err := srv.RunMetrics(cctx.String("metrics-listen")); err != nil {
				slog.Error("failed to start metrics endpoint", "error", err)
				panic(fmt.Errorf("failed to start metrics endpoint: %w", err))
			}
		}()

		// TODO: if cctx.Bool("readonly") ...

		if err := srv.Run(ctx); err != nil {
			return fmt.Errorf("failed to run automod service: %w", err)
		}
		return nil
	},
}
