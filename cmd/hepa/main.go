package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	_ "net/http/pprof"
	"os"
	"runtime"
	"strings"
	"time"

	"github.com/bluesky-social/indigo/atproto/identity"
	"github.com/bluesky-social/indigo/atproto/identity/redisdir"
	"github.com/bluesky-social/indigo/atproto/syntax"
	"github.com/bluesky-social/indigo/automod/capture"
	"github.com/bluesky-social/indigo/automod/consumer"

	"github.com/carlmjohnson/versioninfo"
	_ "github.com/joho/godotenv/autoload"
	cli "github.com/urfave/cli/v2"
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
			Name:    "atp-relay-host",
			Usage:   "hostname and port of Relay to subscribe to",
			Value:   "wss://bsky.network",
			EnvVars: []string{"ATP_RELAY_HOST", "ATP_BGS_HOST"},
		},
		&cli.StringFlag{
			Name:    "atp-plc-host",
			Usage:   "method, hostname, and port of PLC registry",
			Value:   "https://plc.directory",
			EnvVars: []string{"ATP_PLC_HOST"},
		},
		&cli.StringFlag{
			Name:    "atp-bsky-host",
			Usage:   "method, hostname, and port of bsky API (appview) service. does not use auth",
			Value:   "https://public.api.bsky.app",
			EnvVars: []string{"ATP_BSKY_HOST"},
		},
		&cli.StringFlag{
			Name:    "atp-ozone-host",
			Usage:   "method, hostname, and port of ozone instance. requires ozone-admin-token as well",
			Value:   "https://mod.bsky.app",
			EnvVars: []string{"ATP_OZONE_HOST", "ATP_MOD_HOST"},
		},
		&cli.StringFlag{
			Name:    "ozone-did",
			Usage:   "DID of account to attribute ozone actions to",
			EnvVars: []string{"HEPA_OZONE_DID"},
		},
		&cli.StringFlag{
			Name:    "ozone-admin-token",
			Usage:   "admin authentication password for mod service",
			EnvVars: []string{"HEPA_OZONE_AUTH_ADMIN_TOKEN", "HEPA_MOD_AUTH_ADMIN_TOKEN"},
		},
		&cli.StringFlag{
			Name:    "atp-pds-host",
			Usage:   "method, hostname, and port of PDS (or entryway) for admin account info; uses admin auth",
			Value:   "https://bsky.social",
			EnvVars: []string{"ATP_PDS_HOST"},
		},
		&cli.StringFlag{
			Name:    "pds-admin-token",
			Usage:   "admin authentication password for PDS (or entryway)",
			EnvVars: []string{"HEPA_PDS_AUTH_ADMIN_TOKEN"},
		},
		&cli.StringFlag{
			Name:  "redis-url",
			Usage: "redis connection URL",
			// redis://<user>:<pass>@localhost:6379/<db>
			// redis://localhost:6379/0
			EnvVars: []string{"HEPA_REDIS_URL"},
		},
		&cli.IntFlag{
			Name:    "plc-rate-limit",
			Usage:   "max number of requests per second to PLC registry",
			Value:   100,
			EnvVars: []string{"HEPA_PLC_RATE_LIMIT"},
		},
		&cli.StringFlag{
			Name:    "sets-json-path",
			Usage:   "file path of JSON file containing static sets",
			EnvVars: []string{"HEPA_SETS_JSON_PATH"},
		},
		&cli.StringFlag{
			Name:    "hiveai-api-token",
			Usage:   "API token for Hive AI image auto-labeling",
			EnvVars: []string{"HIVEAI_API_TOKEN"},
		},
		&cli.StringFlag{
			Name:    "abyss-host",
			Usage:   "host for abusive image scanning API (scheme, host, port)",
			EnvVars: []string{"ABYSS_HOST"},
		},
		&cli.StringFlag{
			Name:    "abyss-password",
			Usage:   "admin auth password for abyss API",
			EnvVars: []string{"ABYSS_PASSWORD"},
		},
		&cli.StringFlag{
			Name:    "ruleset",
			Usage:   "which ruleset config to use: default, no-blobs, only-blobs",
			EnvVars: []string{"HEPA_RULESET"},
		},
		&cli.StringFlag{
			Name:    "log-level",
			Usage:   "log verbosity level (eg: warn, info, debug)",
			EnvVars: []string{"HEPA_LOG_LEVEL", "LOG_LEVEL"},
		},
		&cli.StringFlag{
			Name:    "ratelimit-bypass",
			Usage:   "HTTP header to bypass ratelimits",
			EnvVars: []string{"HEPA_RATELIMIT_BYPASS", "RATELIMIT_BYPASS"},
		},
		&cli.IntFlag{
			Name:    "firehose-parallelism",
			Usage:   "force a fixed number of parallel firehose workers. default (or 0) for auto-scaling; 200 works for a large instance",
			EnvVars: []string{"HEPA_FIREHOSE_PARALLELISM"},
		},
		&cli.StringFlag{
			Name:    "prescreen-host",
			Usage:   "hostname of prescreen server",
			EnvVars: []string{"HEPA_PRESCREEN_HOST"},
		},
		&cli.StringFlag{
			Name:    "prescreen-token",
			Usage:   "secret token for prescreen server",
			EnvVars: []string{"HEPA_PRESCREEN_TOKEN"},
		},
		&cli.DurationFlag{
			Name:    "report-dupe-period",
			Usage:   "time period within which automod will not re-report an account for the same reasonType",
			EnvVars: []string{"HEPA_REPORT_DUPE_PERIOD"},
			Value:   1 * 24 * time.Hour,
		},
		&cli.IntFlag{
			Name:    "quota-mod-report-day",
			Usage:   "number of reports automod can file per day, for all subjects and types combined (circuit breaker)",
			EnvVars: []string{"HEPA_QUOTA_MOD_REPORT_DAY"},
			Value:   10000,
		},
		&cli.IntFlag{
			Name:    "quota-mod-takedown-day",
			Usage:   "number of takedowns automod can action per day, for all subjects combined (circuit breaker)",
			EnvVars: []string{"HEPA_QUOTA_MOD_TAKEDOWN_DAY"},
			Value:   200,
		},
		&cli.IntFlag{
			Name:    "quota-mod-action-day",
			Usage:   "number of misc actions automod can do per day, for all subjects combined (circuit breaker)",
			EnvVars: []string{"HEPA_QUOTA_MOD_ACTION_DAY"},
			Value:   2000,
		},
		&cli.DurationFlag{
			Name:    "record-event-timeout",
			Usage:   "total processing time for record events (including setup, rules, and persisting)",
			EnvVars: []string{"HEPA_RECORD_EVENT_TIMEOUT"},
			Value:   30 * time.Second,
		},
		&cli.DurationFlag{
			Name:    "identity-event-timeout",
			Usage:   "total processing time for identity and account events (including setup, rules, and persisting)",
			EnvVars: []string{"HEPA_IDENTITY_EVENT_TIMEOUT"},
			Value:   10 * time.Second,
		},
		&cli.DurationFlag{
			Name:    "ozone-event-timeout",
			Usage:   "total processing time for ozone events (including setup, rules, and persisting)",
			EnvVars: []string{"HEPA_OZONE_EVENT_TIMEOUT"},
			Value:   30 * time.Second,
		},
	}

	app.Commands = []*cli.Command{
		runCmd,
		processRecordCmd,
		processRecentCmd,
		captureRecentCmd,
	}

	return app.Run(args)
}

func configDirectory(cctx *cli.Context) (identity.Directory, error) {
	baseDir := identity.BaseDirectory{
		PLCURL: cctx.String("atp-plc-host"),
		HTTPClient: http.Client{
			Timeout: time.Second * 15,
		},
		PLCLimiter:            rate.NewLimiter(rate.Limit(cctx.Int("plc-rate-limit")), 1),
		TryAuthoritativeDNS:   true,
		SkipDNSDomainSuffixes: []string{".bsky.social", ".staging.bsky.dev"},
	}
	var dir identity.Directory
	if cctx.String("redis-url") != "" {
		rdir, err := redisdir.NewRedisDirectory(&baseDir, cctx.String("redis-url"), time.Hour*24, time.Minute*2, time.Minute*5, 10_000)
		if err != nil {
			return nil, err
		}
		dir = rdir
	} else {
		cdir := identity.NewCacheDirectory(&baseDir, 1_500_000, time.Hour*24, time.Minute*2, time.Minute*5)
		dir = &cdir
	}
	return dir, nil
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

var runCmd = &cli.Command{
	Name:  "run",
	Usage: "run the hepa daemon",
	Flags: []cli.Flag{
		&cli.StringFlag{
			Name:    "metrics-listen",
			Usage:   "IP or address, and port, to listen on for metrics APIs",
			Value:   ":3989",
			EnvVars: []string{"HEPA_METRICS_LISTEN"},
		},
		&cli.StringFlag{
			Name: "slack-webhook-url",
			// eg: https://hooks.slack.com/services/X1234
			Usage:   "full URL of slack webhook",
			EnvVars: []string{"SLACK_WEBHOOK_URL"},
		},
	},
	Action: func(cctx *cli.Context) error {
		ctx := context.Background()
		logger := configLogger(cctx, os.Stdout)
		configOTEL("hepa")

		dir, err := configDirectory(cctx)
		if err != nil {
			return fmt.Errorf("failed to configure identity directory: %v", err)
		}

		srv, err := NewServer(
			dir,
			Config{
				Logger:               logger,
				BskyHost:             cctx.String("atp-bsky-host"),
				OzoneHost:            cctx.String("atp-ozone-host"),
				OzoneDID:             cctx.String("ozone-did"),
				OzoneAdminToken:      cctx.String("ozone-admin-token"),
				PDSHost:              cctx.String("atp-pds-host"),
				PDSAdminToken:        cctx.String("pds-admin-token"),
				SetsFileJSON:         cctx.String("sets-json-path"),
				RedisURL:             cctx.String("redis-url"),
				SlackWebhookURL:      cctx.String("slack-webhook-url"),
				HiveAPIToken:         cctx.String("hiveai-api-token"),
				AbyssHost:            cctx.String("abyss-host"),
				AbyssPassword:        cctx.String("abyss-password"),
				RatelimitBypass:      cctx.String("ratelimit-bypass"),
				RulesetName:          cctx.String("ruleset"),
				PreScreenHost:        cctx.String("prescreen-host"),
				PreScreenToken:       cctx.String("prescreen-token"),
				ReportDupePeriod:     cctx.Duration("report-dupe-period"),
				QuotaModReportDay:    cctx.Int("quota-mod-report-day"),
				QuotaModTakedownDay:  cctx.Int("quota-mod-takedown-day"),
				QuotaModActionDay:    cctx.Int("quota-mod-action-day"),
				RecordEventTimeout:   cctx.Duration("record-event-timeout"),
				IdentityEventTimeout: cctx.Duration("identity-event-timeout"),
				OzoneEventTimeout:    cctx.Duration("ozone-event-timeout"),
			},
		)
		if err != nil {
			return fmt.Errorf("failed to construct server: %v", err)
		}

		// ozone event consumer (if configured)
		if srv.Engine.OzoneClient != nil {
			oc := consumer.OzoneConsumer{
				Logger:      logger.With("subsystem", "ozone-consumer"),
				RedisClient: srv.RedisClient,
				OzoneClient: srv.Engine.OzoneClient,
				Engine:      srv.Engine,
			}

			go func() {
				if err := oc.Run(ctx); err != nil {
					slog.Error("ozone consumer failed", "err", err)
				}
			}()

			go func() {
				if err := oc.RunPersistCursor(ctx); err != nil {
					slog.Error("ozone cursor routine failed", "err", err)
				}
			}()
		}

		// prometheus HTTP endpoint: /metrics
		go func() {
			runtime.SetBlockProfileRate(10)
			runtime.SetMutexProfileFraction(10)
			if err := srv.RunMetrics(cctx.String("metrics-listen")); err != nil {
				slog.Error("failed to start metrics endpoint", "error", err)
				panic(fmt.Errorf("failed to start metrics endpoint: %w", err))
			}
		}()

		// firehose event consumer (note this is actually mandatory)
		relayHost := cctx.String("atp-relay-host")
		if relayHost != "" {
			fc := consumer.FirehoseConsumer{
				Engine:      srv.Engine,
				Logger:      logger.With("subsystem", "firehose-consumer"),
				Host:        cctx.String("atp-relay-host"),
				Parallelism: cctx.Int("firehose-parallelism"),
				RedisClient: srv.RedisClient,
			}

			go func() {
				if err := fc.RunPersistCursor(ctx); err != nil {
					slog.Error("cursor routine failed", "err", err)
				}
			}()

			if err := fc.Run(ctx); err != nil {
				return fmt.Errorf("failure consuming and processing firehose: %w", err)
			}
		}

		return nil
	},
}

// for simple commands, not long-running daemons
func configEphemeralServer(cctx *cli.Context) (*Server, error) {
	// NOTE: using stderr not stdout because some commands print to stdout
	logger := configLogger(cctx, os.Stderr)

	dir, err := configDirectory(cctx)
	if err != nil {
		return nil, err
	}

	return NewServer(
		dir,
		Config{
			Logger:          logger,
			BskyHost:        cctx.String("atp-bsky-host"),
			OzoneHost:       cctx.String("atp-ozone-host"),
			OzoneDID:        cctx.String("ozone-did"),
			OzoneAdminToken: cctx.String("ozone-admin-token"),
			PDSHost:         cctx.String("atp-pds-host"),
			PDSAdminToken:   cctx.String("pds-admin-token"),
			SetsFileJSON:    cctx.String("sets-json-path"),
			RedisURL:        cctx.String("redis-url"),
			HiveAPIToken:    cctx.String("hiveai-api-token"),
			AbyssHost:       cctx.String("abyss-host"),
			AbyssPassword:   cctx.String("abyss-password"),
			RatelimitBypass: cctx.String("ratelimit-bypass"),
			RulesetName:     cctx.String("ruleset"),
			PreScreenHost:   cctx.String("prescreen-host"),
			PreScreenToken:  cctx.String("prescreen-token"),
		},
	)
}

var processRecordCmd = &cli.Command{
	Name:      "process-record",
	Usage:     "process a single record in isolation",
	ArgsUsage: `<at-uri>`,
	Flags:     []cli.Flag{},
	Action: func(cctx *cli.Context) error {
		ctx := context.Background()
		uriArg := cctx.Args().First()
		if uriArg == "" {
			return fmt.Errorf("expected a single AT-URI argument")
		}
		aturi, err := syntax.ParseATURI(uriArg)
		if err != nil {
			return fmt.Errorf("not a valid AT-URI: %v", err)
		}

		srv, err := configEphemeralServer(cctx)
		if err != nil {
			return err
		}

		return capture.FetchAndProcessRecord(ctx, srv.Engine, aturi)
	},
}

var processRecentCmd = &cli.Command{
	Name:      "process-recent",
	Usage:     "fetch and process recent posts for an account",
	ArgsUsage: `<at-identifier>`,
	Flags: []cli.Flag{
		&cli.IntFlag{
			Name:  "limit",
			Usage: "how many post records to parse",
			Value: 20,
		},
	},
	Action: func(cctx *cli.Context) error {
		ctx := context.Background()
		idArg := cctx.Args().First()
		if idArg == "" {
			return fmt.Errorf("expected a single AT identifier (handle or DID) argument")
		}
		atid, err := syntax.ParseAtIdentifier(idArg)
		if err != nil {
			return fmt.Errorf("not a valid handle or DID: %v", err)
		}

		srv, err := configEphemeralServer(cctx)
		if err != nil {
			return err
		}

		return capture.FetchAndProcessRecent(ctx, srv.Engine, *atid, cctx.Int("limit"))
	},
}

var captureRecentCmd = &cli.Command{
	Name:      "capture-recent",
	Usage:     "fetch account metadata and recent posts for an account, dump JSON to stdout",
	ArgsUsage: `<at-identifier>`,
	Flags: []cli.Flag{
		&cli.IntFlag{
			Name:  "limit",
			Usage: "how many post records to parse",
			Value: 20,
		},
	},
	Action: func(cctx *cli.Context) error {
		ctx := context.Background()
		idArg := cctx.Args().First()
		if idArg == "" {
			return fmt.Errorf("expected a single AT identifier (handle or DID) argument")
		}
		atid, err := syntax.ParseAtIdentifier(idArg)
		if err != nil {
			return fmt.Errorf("not a valid handle or DID: %v", err)
		}

		srv, err := configEphemeralServer(cctx)
		if err != nil {
			return err
		}

		cap, err := capture.CaptureRecent(ctx, srv.Engine, *atid, cctx.Int("limit"))
		if err != nil {
			return err
		}

		outJSON, err := json.MarshalIndent(cap, "", "  ")
		if err != nil {
			return err
		}

		fmt.Println(string(outJSON))
		return nil
	},
}
