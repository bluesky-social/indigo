package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/bluesky-social/indigo/api"
	libbgs "github.com/bluesky-social/indigo/bgs"
	"github.com/bluesky-social/indigo/blobs"
	"github.com/bluesky-social/indigo/carstore"
	"github.com/bluesky-social/indigo/did"
	"github.com/bluesky-social/indigo/events"
	"github.com/bluesky-social/indigo/indexer"
	"github.com/bluesky-social/indigo/notifs"
	"github.com/bluesky-social/indigo/plc"
	"github.com/bluesky-social/indigo/repomgr"
	"github.com/bluesky-social/indigo/util"
	"github.com/bluesky-social/indigo/util/cliutil"
	"github.com/bluesky-social/indigo/xrpc"
	_ "go.uber.org/automaxprocs"

	"net/http"
	_ "net/http/pprof"

	_ "github.com/joho/godotenv/autoload"

	"github.com/carlmjohnson/versioninfo"
	logging "github.com/ipfs/go-log"
	"github.com/urfave/cli/v2"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/exporters/jaeger"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp"
	"go.opentelemetry.io/otel/sdk/resource"
	tracesdk "go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.4.0"
	"gorm.io/plugin/opentelemetry/tracing"
)

var log = logging.Logger("bigsky")

func init() {
	// control log level using, eg, GOLOG_LOG_LEVEL=debug
	//logging.SetAllLoggers(logging.LevelDebug)
}

func main() {
	run(os.Args)
}

func run(args []string) {
	app := cli.App{
		Name:    "bigsky",
		Usage:   "atproto BGS/firehose daemon",
		Version: versioninfo.Short(),
	}

	app.Flags = []cli.Flag{
		&cli.BoolFlag{
			Name: "jaeger",
		},
		&cli.StringFlag{
			Name:    "db-url",
			Usage:   "database connection string for BGS database",
			Value:   "sqlite://./data/bigsky/bgs.sqlite",
			EnvVars: []string{"DATABASE_URL"},
		},
		&cli.StringFlag{
			Name:    "carstore-db-url",
			Usage:   "database connection string for carstore database",
			Value:   "sqlite://./data/bigsky/carstore.sqlite",
			EnvVars: []string{"CARSTORE_DATABASE_URL"},
		},
		&cli.BoolFlag{
			Name: "db-tracing",
		},
		&cli.StringFlag{
			Name:    "data-dir",
			Usage:   "path of directory for CAR files and other data",
			Value:   "data/bigsky",
			EnvVars: []string{"DATA_DIR"},
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
		&cli.BoolFlag{
			Name:  "aggregation",
			Value: false,
		},
		&cli.BoolFlag{
			Name:    "spidering",
			Value:   true,
			EnvVars: []string{"BGS_SPIDERING"},
		},
		&cli.StringFlag{
			Name:  "api-listen",
			Value: ":2470",
		},
		&cli.StringFlag{
			Name:    "metrics-listen",
			Value:   ":2471",
			EnvVars: []string{"BGS_METRICS_LISTEN"},
		},
		&cli.StringFlag{
			Name: "disk-blob-store",
		},
		&cli.StringFlag{
			Name:  "disk-persister-dir",
			Usage: "set directory for disk persister (implicitly enables disk persister)",
		},
		&cli.StringFlag{
			Name:    "admin-key",
			EnvVars: []string{"BGS_ADMIN_KEY"},
		},
		&cli.StringSliceFlag{
			Name:    "handle-resolver-hosts",
			EnvVars: []string{"HANDLE_RESOLVER_HOSTS"},
		},
		&cli.IntFlag{
			Name:    "max-carstore-connections",
			EnvVars: []string{"MAX_CARSTORE_CONNECTIONS"},
			Value:   40,
		},
		&cli.IntFlag{
			Name:    "max-metadb-connections",
			EnvVars: []string{"MAX_METADB_CONNECTIONS"},
			Value:   40,
		},
		&cli.DurationFlag{
			Name:    "compact-interval",
			EnvVars: []string{"BGS_COMPACT_INTERVAL"},
			Value:   4 * time.Hour,
			Usage:   "interval between compaction runs, set to 0 to disable scheduled compaction",
		},
	}

	app.Action = Bigsky
	err := app.Run(os.Args)
	if err != nil {
		log.Fatal(err)
	}
}

func Bigsky(cctx *cli.Context) error {
	// Trap SIGINT to trigger a shutdown.
	signals := make(chan os.Signal, 1)
	signal.Notify(signals, syscall.SIGINT, syscall.SIGTERM)

	if cctx.Bool("jaeger") {
		url := "http://localhost:14268/api/traces"
		exp, err := jaeger.New(jaeger.WithCollectorEndpoint(jaeger.WithEndpoint(url)))
		if err != nil {
			return err
		}
		tp := tracesdk.NewTracerProvider(
			// Always be sure to batch in production.
			tracesdk.WithBatcher(exp),
			// Record information about this application in a Resource.
			tracesdk.WithResource(resource.NewWithAttributes(
				semconv.SchemaURL,
				semconv.ServiceNameKey.String("bgs"),
				attribute.String("environment", "test"),
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
				semconv.ServiceNameKey.String("bgs"),
				attribute.String("env", os.Getenv("ENVIRONMENT")),         // DataDog
				attribute.String("environment", os.Getenv("ENVIRONMENT")), // Others
				attribute.Int64("ID", 1),
			)),
		)
		otel.SetTracerProvider(tp)
	}

	// ensure data directory exists; won't error if it does
	datadir := cctx.String("data-dir")
	csdir := filepath.Join(datadir, "carstore")
	if err := os.MkdirAll(datadir, os.ModePerm); err != nil {
		return err
	}

	log.Infow("setting up main database")
	dburl := cctx.String("db-url")
	db, err := cliutil.SetupDatabase(dburl, cctx.Int("max-metadb-connections"))
	if err != nil {
		return err
	}

	log.Infow("setting up carstore database")
	csdburl := cctx.String("carstore-db-url")
	csdb, err := cliutil.SetupDatabase(csdburl, cctx.Int("max-carstore-connections"))
	if err != nil {
		return err
	}

	if cctx.Bool("db-tracing") {
		if err := db.Use(tracing.NewPlugin()); err != nil {
			return err
		}
		if err := csdb.Use(tracing.NewPlugin()); err != nil {
			return err
		}
	}

	os.MkdirAll(filepath.Dir(csdir), os.ModePerm)
	csb, err := carstore.NewPostgresBackend(csdb)
	if err != nil {
		return err
	}

	cstore, err := carstore.NewCarStore(csb, csdir)
	if err != nil {
		return err
	}

	mr := did.NewMultiResolver()

	didr := &api.PLCServer{Host: cctx.String("plc-host")}
	mr.AddHandler("plc", didr)

	webr := did.WebResolver{}
	if cctx.Bool("crawl-insecure-ws") {
		webr.Insecure = true
	}
	mr.AddHandler("web", &webr)

	cachedidr := plc.NewCachingDidResolver(mr, time.Hour*24, 500_000)

	kmgr := indexer.NewKeyManager(cachedidr, nil)

	repoman := repomgr.NewRepoManager(cstore, kmgr)

	var persister events.EventPersistence

	if dpd := cctx.String("disk-persister-dir"); dpd != "" {
		log.Infow("setting up disk persister")
		dp, err := events.NewDiskPersistence(dpd, "", db, events.DefaultDiskPersistOptions())
		if err != nil {
			return fmt.Errorf("setting up disk persister: %w", err)
		}
		persister = dp
	} else {
		dbp, err := events.NewDbPersistence(db, cstore, nil)
		if err != nil {
			return fmt.Errorf("setting up db event persistence: %w", err)
		}
		persister = dbp
	}

	evtman := events.NewEventManager(persister)

	notifman := &notifs.NullNotifs{}

	rf := indexer.NewRepoFetcher(db, repoman)

	ix, err := indexer.NewIndexer(db, notifman, evtman, cachedidr, rf, true, cctx.Bool("spidering"), cctx.Bool("aggregation"))
	if err != nil {
		return err
	}

	rlskip := os.Getenv("BSKY_SOCIAL_RATE_LIMIT_SKIP")
	ix.ApplyPDSClientSettings = func(c *xrpc.Client) {
		if c.Host == "https://bsky.social" {
			if c.Client == nil {
				c.Client = util.RobustHTTPClient()
			}
			c.Client.Timeout = time.Minute * 30
			if rlskip != "" {
				c.Headers = map[string]string{
					"x-ratelimit-bypass": rlskip,
				}
			}
		}
	}

	repoman.SetEventHandler(func(ctx context.Context, evt *repomgr.RepoEvent) {
		if err := ix.HandleRepoEvent(ctx, evt); err != nil {
			log.Errorw("failed to handle repo event", "err", err)
		}
	}, false)

	var blobstore blobs.BlobStore
	if bsdir := cctx.String("disk-blob-store"); bsdir != "" {
		blobstore = &blobs.DiskBlobStore{Dir: bsdir}
	}

	prodHR, err := api.NewProdHandleResolver(100_000)
	if err != nil {
		return fmt.Errorf("failed to set up handle resolver: %w", err)
	}
	if rlskip != "" {
		prodHR.ReqMod = func(req *http.Request, host string) error {
			if strings.HasSuffix(host, ".bsky.social") {
				req.Header.Set("x-ratelimit-bypass", rlskip)
			}
			return nil
		}
	}

	var hr api.HandleResolver = prodHR
	if cctx.StringSlice("handle-resolver-hosts") != nil {
		hr = &api.TestHandleResolver{
			TrialHosts: cctx.StringSlice("handle-resolver-hosts"),
		}
	}

	log.Infow("constructing bgs")
	bgs, err := libbgs.NewBGS(db, ix, repoman, evtman, cachedidr, blobstore, rf, hr, !cctx.Bool("crawl-insecure-ws"), cctx.Duration("compact-interval"))
	if err != nil {
		return err
	}

	if tok := cctx.String("admin-key"); tok != "" {
		if err := bgs.CreateAdminToken(tok); err != nil {
			return fmt.Errorf("failed to set up admin token: %w", err)
		}
	}

	// set up metrics endpoint
	go func() {
		if err := bgs.StartMetrics(cctx.String("metrics-listen")); err != nil {
			log.Fatalf("failed to start metrics endpoint: %s", err)
		}
	}()

	bgsErr := make(chan error, 1)

	go func() {
		err := bgs.Start(cctx.String("api-listen"))
		bgsErr <- err
	}()

	log.Infow("startup complete")
	select {
	case <-signals:
		log.Info("received shutdown signal")
		errs := bgs.Shutdown()
		for err := range errs {
			log.Errorw("error during BGS shutdown", "err", err)
		}
	case err := <-bgsErr:
		if err != nil {
			log.Errorw("error during BGS startup", "err", err)
		}
		log.Info("shutting down")
		errs := bgs.Shutdown()
		for err := range errs {
			log.Errorw("error during BGS shutdown", "err", err)
		}
	}

	log.Info("shutdown complete")

	return nil
}
