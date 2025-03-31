package main

import (
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"io"
	"log/slog"
	"net/url"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	_ "github.com/joho/godotenv/autoload"
	_ "go.uber.org/automaxprocs"
	_ "net/http/pprof"

	"github.com/bluesky-social/indigo/atproto/identity"
	"github.com/bluesky-social/indigo/cmd/relayered/slurper"
	"github.com/bluesky-social/indigo/cmd/relayered/stream/eventmgr"
	"github.com/bluesky-social/indigo/cmd/relayered/stream/persist"
	"github.com/bluesky-social/indigo/cmd/relayered/stream/persist/diskpersist"
	"github.com/bluesky-social/indigo/util"
	"github.com/bluesky-social/indigo/util/cliutil"
	"github.com/bluesky-social/indigo/xrpc"

	"github.com/carlmjohnson/versioninfo"
	"github.com/urfave/cli/v2"
	"gorm.io/plugin/opentelemetry/tracing"
)

func main() {
	if err := run(os.Args); err != nil {
		slog.Error(err.Error())
		os.Exit(1)
	}
}

func run(args []string) error {

	app := cli.App{
		Name:    "relay",
		Usage:   "atproto Relay daemon",
		Version: versioninfo.Short(),
	}

	app.Flags = []cli.Flag{
		&cli.BoolFlag{
			Name: "jaeger",
		},
		&cli.StringFlag{
			Name:    "db-url",
			Usage:   "database connection string for relay database",
			Value:   "sqlite://./data/relay/relay.sqlite",
			EnvVars: []string{"DATABASE_URL"},
		},
		&cli.BoolFlag{
			Name: "db-tracing",
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
			Name:    "api-listen",
			Value:   ":2470",
			EnvVars: []string{"RELAY_API_LISTEN"},
		},
		&cli.StringFlag{
			Name:    "metrics-listen",
			Value:   ":2471",
			EnvVars: []string{"RELAY_METRICS_LISTEN"},
		},
		&cli.StringFlag{
			Name:    "disk-persister-dir",
			Usage:   "set directory for disk persister (implicitly enables disk persister)",
			EnvVars: []string{"RELAY_PERSISTER_DIR"},
		},
		&cli.StringFlag{
			Name:    "admin-key",
			EnvVars: []string{"RELAY_ADMIN_KEY"},
		},
		&cli.IntFlag{
			Name:    "max-metadb-connections",
			EnvVars: []string{"MAX_METADB_CONNECTIONS"},
			Value:   40,
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
			EnvVars: []string{"RELAY_DEFAULT_REPO_LIMIT"},
		},
		&cli.IntFlag{
			Name:    "concurrency-per-pds",
			EnvVars: []string{"RELAY_CONCURRENCY_PER_PDS"},
			Value:   100,
		},
		&cli.IntFlag{
			Name:    "max-queue-per-pds",
			EnvVars: []string{"RELAY_MAX_QUEUE_PER_PDS"},
			Value:   1_000,
		},
		&cli.IntFlag{
			Name:    "did-cache-size",
			Usage:   "in-process cache by number of Did documents",
			EnvVars: []string{"RELAY_DID_CACHE_SIZE"},
			Value:   5_000_000,
		},
		&cli.DurationFlag{
			Name:    "event-playback-ttl",
			Usage:   "time to live for event playback buffering (only applies to disk persister)",
			EnvVars: []string{"RELAY_EVENT_PLAYBACK_TTL"},
			Value:   72 * time.Hour,
		},
		&cli.StringSliceFlag{
			Name:    "next-crawler",
			Usage:   "forward POST requestCrawl to this url, should be machine root url and not xrpc/requestCrawl, comma separated list",
			EnvVars: []string{"RELAY_NEXT_CRAWLER"},
		},
		&cli.StringFlag{
			Name:    "trace-induction",
			Usage:   "file path to log debug trace stuff about induction firehose",
			EnvVars: []string{"RELAY_TRACE_INDUCTION"},
		},
		&cli.BoolFlag{
			Name:    "time-seq",
			EnvVars: []string{"RELAY_TIME_SEQUENCE"},
			Value:   false,
			Usage:   "make outbound firehose sequence number approximately unix microseconds",
		},
	}

	app.Action = runRelay
	return app.Run(os.Args)
}

func runRelay(cctx *cli.Context) error {
	// Trap SIGINT to trigger a shutdown.
	signals := make(chan os.Signal, 1)
	signal.Notify(signals, syscall.SIGINT, syscall.SIGTERM)

	logger, logWriter, err := cliutil.SetupSlog(cliutil.LogOptions{})
	if err != nil {
		return err
	}

	var inductionTraceLog *slog.Logger

	if cctx.IsSet("trace-induction") {
		traceFname := cctx.String("trace-induction")
		traceFout, err := os.OpenFile(traceFname, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
		if err != nil {
			return fmt.Errorf("%s: could not open trace file: %w", traceFname, err)
		}
		defer traceFout.Close()
		if traceFname != "" {
			inductionTraceLog = slog.New(slog.NewJSONHandler(traceFout, &slog.HandlerOptions{Level: slog.LevelDebug}))
		}
	} else {
		inductionTraceLog = slog.New(slog.NewTextHandler(io.Discard, &slog.HandlerOptions{Level: slog.Level(999)}))
	}

	// start observability/tracing (OTEL and jaeger)
	if err := setupOTEL(cctx); err != nil {
		return err
	}

	dburl := cctx.String("db-url")
	logger.Info("setting up main database", "url", dburl)
	db, err := cliutil.SetupDatabase(dburl, cctx.Int("max-metadb-connections"))
	if err != nil {
		return err
	}
	if cctx.Bool("db-tracing") {
		if err := db.Use(tracing.NewPlugin()); err != nil {
			return err
		}
	}

	// TODO: add shared external cache
	baseDir := identity.BaseDirectory{
		SkipHandleVerification: true,
		SkipDNSDomainSuffixes:  []string{".bsky.social"},
		TryAuthoritativeDNS:    true,
	}
	cacheDir := identity.NewCacheDirectory(&baseDir, cctx.Int("did-cache-size"), time.Hour*24, time.Minute*2, time.Minute*5)

	// TODO: rename repoman
	repoman := slurper.NewValidator(&cacheDir, inductionTraceLog)

	var persister persist.EventPersistence

	dpd := cctx.String("disk-persister-dir")
	if dpd == "" {
		logger.Info("empty disk-persister-dir, use current working directory")
		cwd, err := os.Getwd()
		if err != nil {
			return err
		}
		dpd = filepath.Join(cwd, "relay-persist")
	}
	logger.Info("setting up disk persister", "dir", dpd)

	pOpts := diskpersist.DefaultDiskPersistOptions()
	pOpts.Retention = cctx.Duration("event-playback-ttl")
	pOpts.TimeSequence = cctx.Bool("time-seq")

	dp, err := diskpersist.NewDiskPersistence(dpd, "", db, pOpts)
	if err != nil {
		return fmt.Errorf("setting up disk persister: %w", err)
	}
	persister = dp

	evtman := eventmgr.NewEventManager(persister)

	ratelimitBypass := cctx.String("bsky-social-rate-limit-skip")

	logger.Info("constructing relay service")
	svcConfig := DefaultServiceConfig()
	svcConfig.SSL = !cctx.Bool("crawl-insecure-ws")
	svcConfig.ConcurrencyPerPDS = cctx.Int64("concurrency-per-pds")
	svcConfig.MaxQueuePerPDS = cctx.Int64("max-queue-per-pds")
	svcConfig.DefaultRepoLimit = cctx.Int64("default-repo-limit")
	svcConfig.ApplyPDSClientSettings = makePdsClientSetup(ratelimitBypass)
	svcConfig.InductionTraceLog = inductionTraceLog
	nextCrawlers := cctx.StringSlice("next-crawler")
	if len(nextCrawlers) != 0 {
		nextCrawlerUrls := make([]*url.URL, len(nextCrawlers))
		for i, tu := range nextCrawlers {
			var err error
			nextCrawlerUrls[i], err = url.Parse(tu)
			if err != nil {
				return fmt.Errorf("failed to parse next-crawler url: %w", err)
			}
			logger.Info("configuring relay for requestCrawl", "host", nextCrawlerUrls[i])
		}
		svcConfig.NextCrawlers = nextCrawlerUrls
	}
	if cctx.IsSet("admin-key") {
		svcConfig.AdminToken = cctx.String("admin-key")
	} else {
		var rblob [10]byte
		_, _ = rand.Read(rblob[:])
		svcConfig.AdminToken = base64.URLEncoding.EncodeToString(rblob[:])
		logger.Info("generated random admin key", "header", "Authorization: Bearer "+svcConfig.AdminToken)
	}
	svc, err := NewService(db, repoman, evtman, &cacheDir, svcConfig)
	if err != nil {
		return err
	}
	dp.SetUidSource(svc)

	// set up metrics endpoint
	go func() {
		if err := svc.StartMetrics(cctx.String("metrics-listen")); err != nil {
			logger.Error("failed to start metrics endpoint", "err", err)
			os.Exit(1)
		}
	}()

	svcErr := make(chan error, 1)

	go func() {
		err := svc.Start(cctx.String("api-listen"), logWriter)
		svcErr <- err
	}()

	logger.Info("startup complete")
	select {
	case <-signals:
		logger.Info("received shutdown signal")
		errs := svc.Shutdown()
		for err := range errs {
			logger.Error("error during shutdown", "err", err)
		}
	case err := <-svcErr:
		if err != nil {
			logger.Error("error during startup", "err", err)
		}
		logger.Info("shutting down")
		errs := svc.Shutdown()
		for err := range errs {
			logger.Error("error during shutdown", "err", err)
		}
	}

	logger.Info("shutdown complete")

	return nil
}

func makePdsClientSetup(ratelimitBypass string) func(c *xrpc.Client) {
	return func(c *xrpc.Client) {
		if c.Client == nil {
			c.Client = util.RobustHTTPClient()
		}
		if strings.HasSuffix(c.Host, ".bsky.network") {
			c.Client.Timeout = time.Minute * 30
			if ratelimitBypass != "" {
				c.Headers = map[string]string{
					"x-ratelimit-bypass": ratelimitBypass,
				}
			}
		} else {
			// Generic PDS timeout
			c.Client.Timeout = time.Minute * 1
		}
	}
}
