package main

import (
	"context"
	"fmt"
	"log/slog"
	_ "net/http/pprof"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	archiver "github.com/bluesky-social/indigo/archiver"
	"github.com/bluesky-social/indigo/did"
	"github.com/bluesky-social/indigo/indexer"
	"github.com/bluesky-social/indigo/plc"
	"github.com/bluesky-social/indigo/repomgr"
	"github.com/bluesky-social/indigo/repostore"
	"github.com/bluesky-social/indigo/util"
	"github.com/bluesky-social/indigo/util/cliutil"
	"github.com/bluesky-social/indigo/xrpc"

	_ "github.com/joho/godotenv/autoload"
	_ "go.uber.org/automaxprocs"

	"github.com/carlmjohnson/versioninfo"
	"github.com/urfave/cli/v2"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/exporters/jaeger"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp"
	"go.opentelemetry.io/otel/sdk/resource"
	tracesdk "go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.4.0"
)

var log = slog.Default().With("system", "archiver")

func main() {
	if err := run(os.Args); err != nil {
		slog.Error(err.Error())
		os.Exit(1)
	}
}

func run(args []string) error {

	app := cli.App{
		Name:    "archiver",
		Usage:   "atproto repo archiver daemon",
		Version: versioninfo.Short(),
	}

	app.Flags = []cli.Flag{
		&cli.BoolFlag{
			Name: "jaeger",
		},
		&cli.StringFlag{
			Name:    "db-url",
			Usage:   "database connection string for database",
			Value:   "sqlite://./data/archiver/db.sqlite",
			EnvVars: []string{"DATABASE_URL"},
		},
		&cli.BoolFlag{
			Name: "db-tracing",
		},
		&cli.StringFlag{
			Name:    "data-dir",
			Usage:   "path of directory for CAR files and other data",
			Value:   "data/archiver",
			EnvVars: []string{"ARCHIVER_DATA_DIR", "DATA_DIR"},
		},
		&cli.StringFlag{
			Name:    "plc-host",
			Usage:   "method, hostname, and port of PLC registry",
			Value:   "https://plc.directory",
			EnvVars: []string{"ATP_PLC_HOST"},
		},
		&cli.BoolFlag{
			Name:  "crawl-insecure-ws",
			Usage: "when connecting to PDS instances, use ws:// instead of wss://",
		},
		&cli.StringFlag{
			Name:  "api-listen",
			Value: ":2970",
		},
		&cli.StringFlag{
			Name:    "metrics-listen",
			Value:   ":2971",
			EnvVars: []string{"ARCHIVER_METRICS_LISTEN", "BGS_METRICS_LISTEN"},
		},
		&cli.StringFlag{
			Name:    "admin-key",
			EnvVars: []string{"ARCHIVER_ADMIN_KEY", "BGS_ADMIN_KEY"},
		},
		&cli.StringSliceFlag{
			Name:    "handle-resolver-hosts",
			EnvVars: []string{"HANDLE_RESOLVER_HOSTS"},
		},
		&cli.IntFlag{
			Name:    "max-db-connections",
			EnvVars: []string{"MAX_METADB_CONNECTIONS"},
			Value:   40,
		},
		&cli.DurationFlag{
			Name:    "compact-interval",
			EnvVars: []string{"ARCHIVER_COMPACT_INTERVAL", "BGS_COMPACT_INTERVAL"},
			Value:   4 * time.Hour,
			Usage:   "interval between compaction runs, set to 0 to disable scheduled compaction",
		},
		&cli.StringFlag{
			Name:    "resolve-address",
			EnvVars: []string{"RESOLVE_ADDRESS"},
			Value:   "1.1.1.1:53",
		},
		&cli.BoolFlag{
			Name:    "force-dns-udp",
			EnvVars: []string{"FORCE_DNS_UDP"},
		},
		&cli.IntFlag{
			Name:    "max-fetch-concurrency",
			Value:   100,
			EnvVars: []string{"MAX_FETCH_CONCURRENCY"},
		},
		&cli.StringFlag{
			Name:    "env",
			Value:   "dev",
			EnvVars: []string{"ENVIRONMENT"},
			Usage:   "declared hosting environment (prod, qa, etc); used in metrics",
		},
		&cli.StringFlag{
			Name:    "otel-exporter-otlp-endpoint",
			EnvVars: []string{"OTEL_EXPORTER_OTLP_ENDPOINT"},
		},
		&cli.StringFlag{
			Name:    "bsky-social-rate-limit-skip",
			EnvVars: []string{"BSKY_SOCIAL_RATE_LIMIT_SKIP"},
			Usage:   "ratelimit bypass secret token for *.bsky.social domains",
		},
		&cli.IntFlag{
			Name:    "default-repo-limit",
			Value:   100,
			EnvVars: []string{"ARCHIVER_DEFAULT_REPO_LIMIT"},
		},
		&cli.IntFlag{
			Name:    "concurrency-per-pds",
			EnvVars: []string{"ARCHIVER_CONCURRENCY_PER_PDS"},
			Value:   100,
		},
		&cli.IntFlag{
			Name:    "max-queue-per-pds",
			EnvVars: []string{"ARCHIVER_MAX_QUEUE_PER_PDS"},
			Value:   1_000,
		},
		&cli.IntFlag{
			Name:    "did-cache-size",
			EnvVars: []string{"ARCHIVER_DID_CACHE_SIZE"},
			Value:   5_000_000,
		},
		&cli.StringSliceFlag{
			Name:    "did-memcached",
			EnvVars: []string{"ARCHIVER_DID_MEMCACHED"},
		},
		&cli.IntFlag{
			Name:    "num-compaction-workers",
			EnvVars: []string{"ARCHIVER_NUM_COMPACTION_WORKERS"},
			Value:   2,
		},
		&cli.StringSliceFlag{
			Name:    "carstore-shard-dirs",
			Usage:   "specify list of shard directories for carstore storage, overrides default storage within datadir",
			EnvVars: []string{"ARCHIVER_CARSTORE_SHARD_DIRS"},
		},
	}

	app.Action = runBigsky
	return app.Run(args)
}

func setupOTEL(cctx *cli.Context) error {

	env := cctx.String("env")
	if env == "" {
		env = "dev"
	}
	if cctx.Bool("jaeger") {
		jaegerUrl := "http://localhost:14268/api/traces"
		exp, err := jaeger.New(jaeger.WithCollectorEndpoint(jaeger.WithEndpoint(jaegerUrl)))
		if err != nil {
			return err
		}
		tp := tracesdk.NewTracerProvider(
			// Always be sure to batch in production.
			tracesdk.WithBatcher(exp),
			// Record information about this application in a Resource.
			tracesdk.WithResource(resource.NewWithAttributes(
				semconv.SchemaURL,
				semconv.ServiceNameKey.String("arc"),
				attribute.String("env", env),         // DataDog
				attribute.String("environment", env), // Others
				attribute.Int64("ID", 1),
			)),
		)

		otel.SetTracerProvider(tp)
	}

	// Enable OTLP HTTP exporter
	// For relevant environment variables:
	// https://pkg.go.dev/go.opentelemetry.io/otel/exporters/otlp/otlptrace#readme-environment-variables
	// At a minimum, you need to set
	// OTEL_EXPORTER_OTLP_ENDPOINT=http://localhost:4318
	if ep := cctx.String("otel-exporter-otlp-endpoint"); ep != "" {
		slog.Info("setting up trace exporter", "endpoint", ep)
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()

		exp, err := otlptracehttp.New(ctx)
		if err != nil {
			slog.Error("failed to create trace exporter", "error", err)
			os.Exit(1)
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
				semconv.ServiceNameKey.String("archiver"),
				attribute.String("env", env),         // DataDog
				attribute.String("environment", env), // Others
				attribute.Int64("ID", 1),
			)),
		)
		otel.SetTracerProvider(tp)
	}

	return nil
}

func runBigsky(cctx *cli.Context) error {
	// Trap SIGINT to trigger a shutdown.
	signals := make(chan os.Signal, 1)
	signal.Notify(signals, syscall.SIGINT, syscall.SIGTERM)

	_, _, err := cliutil.SetupSlog(cliutil.LogOptions{})
	if err != nil {
		return err
	}

	// start observability/tracing (OTEL and jaeger)
	if err := setupOTEL(cctx); err != nil {
		return err
	}

	// ensure data directory exists; won't error if it does
	datadir := cctx.String("data-dir")
	csdir := filepath.Join(datadir, "carstore")
	if err := os.MkdirAll(datadir, os.ModePerm); err != nil {
		return err
	}

	dburl := cctx.String("db-url")
	slog.Info("setting up main database", "url", dburl)
	db, err := cliutil.SetupDatabase(dburl, cctx.Int("max-db-connections"))
	if err != nil {
		return err
	}

	// make standard FileCarStore
	csdirs := []string{csdir}
	if paramDirs := cctx.StringSlice("carstore-shard-dirs"); len(paramDirs) > 0 {
		csdirs = paramDirs
	}

	for _, csd := range csdirs {
		if err := os.MkdirAll(filepath.Dir(csd), os.ModePerm); err != nil {
			return err
		}
	}

	cstore, err := repostore.NewRepoStore(db, csdirs)
	if err != nil {
		return err
	}

	// DID RESOLUTION
	// 1. the outside world, PLCSerever or Web
	// 2. (maybe memcached)
	// 3. in-process cache
	var cachedidr did.Resolver
	{
		mr := did.NewMultiResolver()

		didr := &plc.PLCServer{Host: cctx.String("plc-host")}
		mr.AddHandler("plc", didr)

		webr := did.WebResolver{}
		if cctx.Bool("crawl-insecure-ws") {
			webr.Insecure = true
		}
		mr.AddHandler("web", &webr)

		var prevResolver did.Resolver
		memcachedServers := cctx.StringSlice("did-memcached")
		if len(memcachedServers) > 0 {
			prevResolver = plc.NewMemcachedDidResolver(mr, time.Hour*24, memcachedServers)
		} else {
			prevResolver = mr
		}

		cachedidr = plc.NewCachingDidResolver(prevResolver, time.Hour*24, cctx.Int("did-cache-size"))
	}

	kmgr := indexer.NewKeyManager(cachedidr, nil)

	repoman := repomgr.NewRepoManager(cstore, kmgr)

	rf := archiver.NewRepoFetcher(db, repoman, cctx.Int("max-fetch-concurrency"))

	rlskip := cctx.String("bsky-social-rate-limit-skip")
	rf.ApplyPDSClientSettings = func(c *xrpc.Client) {
		if c.Client == nil {
			c.Client = util.RobustHTTPClient()
		}
		if strings.HasSuffix(c.Host, ".bsky.network") {
			c.Client.Timeout = time.Minute * 30
			if rlskip != "" {
				c.Headers = map[string]string{
					"x-ratelimit-bypass": rlskip,
				}
			}
		} else {
			// Generic PDS timeout
			c.Client.Timeout = time.Minute * 1
		}
	}

	slog.Info("constructing archiver")
	archiverConfig := archiver.DefaultArchiverConfig()
	archiverConfig.SSL = !cctx.Bool("crawl-insecure-ws")
	archiverConfig.CompactInterval = cctx.Duration("compact-interval")
	archiverConfig.ConcurrencyPerPDS = cctx.Int64("concurrency-per-pds")
	archiverConfig.MaxQueuePerPDS = cctx.Int64("max-queue-per-pds")
	archiverConfig.DefaultRepoLimit = cctx.Int64("default-repo-limit")
	archiverConfig.NumCompactionWorkers = cctx.Int("num-compaction-workers")

	arc, err := archiver.NewArchiver(db, repoman, cachedidr, rf, archiverConfig)
	if err != nil {
		return err
	}

	if tok := cctx.String("admin-key"); tok != "" {
		if err := arc.CreateAdminToken(tok); err != nil {
			return fmt.Errorf("failed to set up admin token: %w", err)
		}
	}

	// set up metrics endpoint
	go func() {
		if err := arc.StartMetrics(cctx.String("metrics-listen")); err != nil {
			log.Error("failed to start metrics endpoint", "err", err)
			os.Exit(1)
		}
	}()

	arcErr := make(chan error, 1)

	go func() {
		err := arc.Start(cctx.String("api-listen"))
		arcErr <- err
	}()

	slog.Info("startup complete")
	select {
	case <-signals:
		log.Info("received shutdown signal")
		errs := arc.Shutdown()
		for err := range errs {
			slog.Error("error during BGS shutdown", "err", err)
		}
	case err := <-arcErr:
		if err != nil {
			slog.Error("error during BGS startup", "err", err)
		}
		log.Info("shutting down")
		errs := arc.Shutdown()
		for err := range errs {
			slog.Error("error during BGS shutdown", "err", err)
		}
	}

	log.Info("shutdown complete")

	return nil
}
