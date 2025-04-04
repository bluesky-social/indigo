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
	"strings"
	"syscall"
	"time"

	_ "github.com/joho/godotenv/autoload"
	_ "go.uber.org/automaxprocs"
	_ "net/http/pprof"

	"github.com/bluesky-social/indigo/atproto/identity"
	"github.com/bluesky-social/indigo/cmd/rerelay/relay"
	"github.com/bluesky-social/indigo/cmd/rerelay/stream/eventmgr"
	"github.com/bluesky-social/indigo/cmd/rerelay/stream/persist/diskpersist"
	"github.com/bluesky-social/indigo/util/cliutil"

	"github.com/carlmjohnson/versioninfo"
	"github.com/urfave/cli/v2"
	"gorm.io/plugin/opentelemetry/tracing"
)

func main() {
	if err := run(os.Args); err != nil {
		slog.Error("exiting process", "err", err.Error())
		os.Exit(-1)
	}
}

func run(args []string) error {

	app := cli.App{
		Name:    "rerelay",
		Usage:   "atproto relay daemon",
		Version: versioninfo.Short(),
	}
	app.Flags = []cli.Flag{
		// XXX: actually disabled if empty?
		&cli.StringFlag{
			Name:    "admin-password",
			Usage:   "secret password/token for accessing admin endpoints (random is used if not set)",
			EnvVars: []string{"RELAY_ADMIN_PASSWORD", "RELAY_ADMIN_KEY"},
		},
		// XXX: not used?
		&cli.StringFlag{
			Name:    "plc-host",
			Usage:   "method, hostname, and port of PLC registry",
			Value:   "https://plc.directory",
			EnvVars: []string{"ATP_PLC_HOST"},
		},
		&cli.StringFlag{
			Name:    "log-level",
			Usage:   "log verbosity level (eg: warn, info, debug)",
			EnvVars: []string{"BLUEPAGES_LOG_LEVEL", "GO_LOG_LEVEL", "LOG_LEVEL"},
		},
	}
	app.Commands = []*cli.Command{
		&cli.Command{
			Name:   "serve",
			Usage:  "run the relay daemon",
			Action: runRelay,
			Flags: []cli.Flag{
				&cli.StringFlag{
					Name:    "db-url",
					Usage:   "database connection string for relay database",
					Value:   "sqlite://data/relay/relay.sqlite",
					EnvVars: []string{"DATABASE_URL"},
				},
				&cli.IntFlag{
					Name:    "max-db-conn",
					Usage:   "limit on size of database connection pool",
					EnvVars: []string{"MAX_DB_CONNECTIONS", "MAX_METADB_CONNECTIONS"},
					Value:   40,
				},
				&cli.StringFlag{
					Name:    "bind",
					Usage:   "IP or address, and port, to listen on for HTTP APIs (including firehose)",
					Value:   ":2470",
					EnvVars: []string{"RELAY_API_BIND", "RELAY_API_LISTEN"},
				},
				&cli.StringFlag{
					Name:    "persist-dir",
					Usage:   "local folder to store firehose playback files",
					Value:   "data/relay/events",
					EnvVars: []string{"RELAY_PERSIST_DIR", "RELAY_PERSISTER_DIR"},
				},
				&cli.DurationFlag{
					Name:    "replay-window",
					Usage:   "retention duration for firehose playback",
					EnvVars: []string{"RELAY_REPLAY_WINDOW", "RELAY_EVENT_PLAYBACK_TTL"},
					Value:   72 * time.Hour,
				},
				&cli.IntFlag{
					Name:    "host-concurrency",
					Usage:   "number of concurrent worker routines per upstream host",
					EnvVars: []string{"RELAY_HOST_CONCURRENCY", "RELAY_CONCURRENCY_PER_PDS"},
					Value:   100,
				},
				&cli.IntFlag{
					Name:    "default-account-limit",
					Value:   100,
					Usage:   "max number of active accounts for new upstream hosts",
					EnvVars: []string{"RELAY_DEFAULT_ACCOUUNT_LIMIT", "RELAY_DEFAULT_REPO_LIMIT"},
				},
				&cli.IntFlag{
					Name:    "did-cache-size",
					Value:   5_000_000,
					Usage:   "size of in-process DID (identity) cache",
					EnvVars: []string{"RELAY_DID_CACHE_SIZE"},
				},
				&cli.StringFlag{
					Name:    "env",
					Value:   "dev",
					EnvVars: []string{"ENVIRONMENT"},
					Usage:   "declared hosting environment (prod, qa, etc); used in metrics",
				},
				&cli.BoolFlag{
					Name: "enable-db-tracing",
				},
				&cli.BoolFlag{
					Name: "enable-jaeger-tracing",
				},
				&cli.BoolFlag{
					Name: "enable-otel-tracing",
				},
				&cli.StringFlag{
					Name:    "metrics-listen",
					Usage:   "IP or address, and port, to listen on for prometheus metrics",
					Value:   ":2471",
					EnvVars: []string{"RELAY_METRICS_LISTEN"},
				},
				&cli.StringFlag{
					Name:    "otel-exporter-otlp-endpoint",
					Value:   "http://localhost:4328",
					EnvVars: []string{"OTEL_EXPORTER_OTLP_ENDPOINT"},
				},
				// XXX: refactor this flag
				&cli.BoolFlag{
					Name:  "crawl-insecure-ws",
					Usage: "when connecting to PDS instances, use ws:// instead of wss://",
				},
				&cli.StringSliceFlag{
					Name:    "forward-crawl-requests",
					Usage:   "comma-separated list of servers (eg https://example.com) to forward requestCrawl on to",
					EnvVars: []string{"RELAY_FORWARD_CRAWL_REQUESTS", "RELAY_NEXT_CRAWLER"},
				},
				&cli.StringFlag{
					Name:    "bsky-social-rate-limit-skip",
					EnvVars: []string{"BSKY_SOCIAL_RATE_LIMIT_SKIP"},
					Usage:   "ratelimit bypass secret token for *.bsky.social domains",
				},
			},
		},
	}
	return app.Run(os.Args)

}

func configLogger(cctx *cli.Context, writer io.Writer) *slog.Logger {
	var level slog.Level
	switch strings.ToLower(cctx.String("log-level")) {
	case "error":
		level = slog.LevelError
	case "warn":
		level = slog.LevelWarn
	case "info":
		level = slog.LevelInfo
	case "debug":
		level = slog.LevelDebug
	default:
		level = slog.LevelInfo
	}
	logger := slog.New(slog.NewJSONHandler(writer, &slog.HandlerOptions{
		Level: level,
	}))
	slog.SetDefault(logger)
	return logger
}

func runRelay(cctx *cli.Context) error {
	logger := configLogger(cctx, os.Stdout)

	// Trap SIGINT to trigger a shutdown.
	signals := make(chan os.Signal, 1)
	signal.Notify(signals, syscall.SIGINT, syscall.SIGTERM)

	dburl := cctx.String("db-url")
	maxConn := cctx.Int("max-db-conn")
	logger.Info("configuring database", "url", dburl, "maxConn", maxConn)
	db, err := cliutil.SetupDatabase(dburl, maxConn)
	if err != nil {
		return err
	}

	// TODO: add shared external cache
	baseDir := identity.BaseDirectory{
		SkipHandleVerification: true,
		SkipDNSDomainSuffixes:  []string{".bsky.social"},
		TryAuthoritativeDNS:    true,
	}
	dir := identity.NewCacheDirectory(&baseDir, cctx.Int("did-cache-size"), time.Hour*24, time.Minute*2, time.Minute*5)

	persistDir := cctx.String("persist-dir")
	os.MkdirAll(persistDir, os.ModePerm)
	persitConfig := diskpersist.DefaultDiskPersistOptions()
	persitConfig.Retention = cctx.Duration("replay-window")
	logger.Info("setting up disk persister", "dir", persistDir, "replayWindow", persitConfig.Retention)
	persister, err := diskpersist.NewDiskPersistence(persistDir, "", db, persitConfig)
	if err != nil {
		return fmt.Errorf("setting up disk persister: %w", err)
	}

	svcConfig := DefaultServiceConfig()
	relayConfig := relay.DefaultRelayConfig()
	relayConfig.SSL = !cctx.Bool("crawl-insecure-ws")
	relayConfig.ConcurrencyPerHost = cctx.Int64("host-concurrency")
	relayConfig.DefaultRepoLimit = cctx.Int64("default-account-limit")
	ratelimitBypass := cctx.String("bsky-social-rate-limit-skip")
	// TODO: actually use ratelimitBypass for host checks?
	_ = ratelimitBypass
	nextCrawlers := cctx.StringSlice("forward-crawl-requests")
	if len(nextCrawlers) > 0 {
		nextCrawlerUrls := make([]*url.URL, len(nextCrawlers))
		for i, tu := range nextCrawlers {
			var err error
			nextCrawlerUrls[i], err = url.Parse(tu)
			if err != nil {
				return fmt.Errorf("invalid crawl request forwarding URL: %w", err)
			}
		}
		svcConfig.NextCrawlers = nextCrawlerUrls
		logger.Info("crawl request forwarding enabled", "servers", svcConfig.NextCrawlers)
	}
	if cctx.IsSet("admin-password") {
		svcConfig.AdminPassword = cctx.String("admin-password")
	} else {
		var rblob [10]byte
		_, _ = rand.Read(rblob[:])
		svcConfig.AdminPassword = base64.URLEncoding.EncodeToString(rblob[:])
		logger.Info("generated random admin password", "username", "admin", "password", svcConfig.AdminPassword)
	}

	evtman := eventmgr.NewEventManager(persister)

	logger.Info("constructing relay service")
	r, err := relay.NewRelay(db, evtman, &dir, relayConfig)
	if err != nil {
		return err
	}
	svc, err := NewService(db, r, svcConfig)
	if err != nil {
		return err
	}
	persister.SetUidSource(r)

	// start metrics endpoint
	go func() {
		if err := svc.StartMetrics(cctx.String("metrics-listen")); err != nil {
			logger.Error("failed to start metrics endpoint", "err", err)
			os.Exit(1)
		}
	}()

	// start observability/tracing (OTEL and jaeger)
	if err := setupOTEL(cctx); err != nil {
		return err
	}
	if cctx.Bool("enable-db-tracing") {
		if err := db.Use(tracing.NewPlugin()); err != nil {
			return err
		}
	}

	svcErr := make(chan error, 1)

	go func() {
		err := svc.StartAPI(cctx.String("bind"))
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
