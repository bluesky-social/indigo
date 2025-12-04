package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"runtime"
	"strings"
	"time"

	_ "github.com/joho/godotenv/autoload"
	_ "net/http/pprof"

	"github.com/bluesky-social/indigo/atproto/identity"
	"github.com/bluesky-social/indigo/atproto/identity/redisdir"
	"github.com/bluesky-social/indigo/atproto/syntax"
	"github.com/bluesky-social/indigo/automod/capture"
	"github.com/bluesky-social/indigo/automod/consumer"

	"github.com/earthboundkid/versioninfo/v2"
	"github.com/urfave/cli/v3"
	"golang.org/x/time/rate"
)

func main() {
	if err := run(os.Args); err != nil {
		slog.Error("exiting", "err", err)
		os.Exit(-1)
	}
}

func run(args []string) error {

	app := cli.Command{
		Name:    "hepa",
		Usage:   "automod daemon (cleans the atmosphere)",
		Version: versioninfo.Short(),
	}

	app.Flags = []cli.Flag{
		&cli.StringFlag{
			Name:    "atp-relay-host",
			Usage:   "hostname and port of Relay to subscribe to",
			Value:   "wss://bsky.network",
			Sources: cli.EnvVars("ATP_RELAY_HOST", "ATP_BGS_HOST"),
		},
		&cli.StringFlag{
			Name:    "atp-plc-host",
			Usage:   "method, hostname, and port of PLC registry",
			Value:   "https://plc.directory",
			Sources: cli.EnvVars("ATP_PLC_HOST"),
		},
		&cli.StringFlag{
			Name:    "atp-bsky-host",
			Usage:   "method, hostname, and port of bsky API (appview) service. does not use auth",
			Value:   "https://public.api.bsky.app",
			Sources: cli.EnvVars("ATP_BSKY_HOST"),
		},
		&cli.StringFlag{
			Name:    "atp-ozone-host",
			Usage:   "method, hostname, and port of ozone instance. requires ozone-admin-token as well",
			Value:   "https://mod.bsky.app",
			Sources: cli.EnvVars("ATP_OZONE_HOST", "ATP_MOD_HOST"),
		},
		&cli.StringFlag{
			Name:    "ozone-did",
			Usage:   "DID of account to attribute ozone actions to",
			Sources: cli.EnvVars("HEPA_OZONE_DID"),
		},
		&cli.StringFlag{
			Name:    "ozone-admin-token",
			Usage:   "admin authentication password for mod service",
			Sources: cli.EnvVars("HEPA_OZONE_AUTH_ADMIN_TOKEN", "HEPA_MOD_AUTH_ADMIN_TOKEN"),
		},
		&cli.StringFlag{
			Name:    "atp-pds-host",
			Usage:   "method, hostname, and port of PDS (or entryway) for admin account info; uses admin auth",
			Value:   "https://bsky.social",
			Sources: cli.EnvVars("ATP_PDS_HOST"),
		},
		&cli.StringFlag{
			Name:    "pds-admin-token",
			Usage:   "admin authentication password for PDS (or entryway)",
			Sources: cli.EnvVars("HEPA_PDS_AUTH_ADMIN_TOKEN"),
		},
		&cli.StringFlag{
			Name:  "redis-url",
			Usage: "redis connection URL",
			// redis://<user>:<pass>@localhost:6379/<db>
			// redis://localhost:6379/0
			Sources: cli.EnvVars("HEPA_REDIS_URL"),
		},
		&cli.IntFlag{
			Name:    "plc-rate-limit",
			Usage:   "max number of requests per second to PLC registry",
			Value:   100,
			Sources: cli.EnvVars("HEPA_PLC_RATE_LIMIT"),
		},
		&cli.StringFlag{
			Name:    "sets-json-path",
			Usage:   "file path of JSON file containing static sets",
			Sources: cli.EnvVars("HEPA_SETS_JSON_PATH"),
		},
		&cli.StringFlag{
			Name:    "hiveai-api-token",
			Usage:   "API token for Hive AI image auto-labeling",
			Sources: cli.EnvVars("HIVEAI_API_TOKEN"),
		},
		&cli.StringFlag{
			Name:    "abyss-host",
			Usage:   "host for abusive image scanning API (scheme, host, port)",
			Sources: cli.EnvVars("ABYSS_HOST"),
		},
		&cli.StringFlag{
			Name:    "abyss-password",
			Usage:   "admin auth password for abyss API",
			Sources: cli.EnvVars("ABYSS_PASSWORD"),
		},
		&cli.StringFlag{
			Name:    "ruleset",
			Usage:   "which ruleset config to use: default, no-blobs, only-blobs",
			Sources: cli.EnvVars("HEPA_RULESET"),
		},
		&cli.StringFlag{
			Name:    "log-level",
			Usage:   "log verbosity level (eg: warn, info, debug)",
			Sources: cli.EnvVars("HEPA_LOG_LEVEL", "LOG_LEVEL"),
		},
		&cli.StringFlag{
			Name:    "ratelimit-bypass",
			Usage:   "HTTP header to bypass ratelimits",
			Sources: cli.EnvVars("HEPA_RATELIMIT_BYPASS", "RATELIMIT_BYPASS"),
		},
		&cli.IntFlag{
			Name:    "firehose-parallelism",
			Usage:   "force a fixed number of parallel firehose workers. default (or 0) for auto-scaling; 200 works for a large instance",
			Sources: cli.EnvVars("HEPA_FIREHOSE_PARALLELISM"),
		},
		&cli.StringFlag{
			Name:    "prescreen-host",
			Usage:   "hostname of prescreen server",
			Sources: cli.EnvVars("HEPA_PRESCREEN_HOST"),
		},
		&cli.StringFlag{
			Name:    "prescreen-token",
			Usage:   "secret token for prescreen server",
			Sources: cli.EnvVars("HEPA_PRESCREEN_TOKEN"),
		},
		&cli.DurationFlag{
			Name:    "report-dupe-period",
			Usage:   "time period within which automod will not re-report an account for the same reasonType",
			Sources: cli.EnvVars("HEPA_REPORT_DUPE_PERIOD"),
			Value:   1 * 24 * time.Hour,
		},
		&cli.IntFlag{
			Name:    "quota-mod-report-day",
			Usage:   "number of reports automod can file per day, for all subjects and types combined (circuit breaker)",
			Sources: cli.EnvVars("HEPA_QUOTA_MOD_REPORT_DAY"),
			Value:   10000,
		},
		&cli.IntFlag{
			Name:    "quota-mod-takedown-day",
			Usage:   "number of takedowns automod can action per day, for all subjects combined (circuit breaker)",
			Sources: cli.EnvVars("HEPA_QUOTA_MOD_TAKEDOWN_DAY"),
			Value:   200,
		},
		&cli.IntFlag{
			Name:    "quota-mod-action-day",
			Usage:   "number of misc actions automod can do per day, for all subjects combined (circuit breaker)",
			Sources: cli.EnvVars("HEPA_QUOTA_MOD_ACTION_DAY"),
			Value:   2000,
		},
		&cli.DurationFlag{
			Name:    "record-event-timeout",
			Usage:   "total processing time for record events (including setup, rules, and persisting)",
			Sources: cli.EnvVars("HEPA_RECORD_EVENT_TIMEOUT"),
			Value:   30 * time.Second,
		},
		&cli.DurationFlag{
			Name:    "identity-event-timeout",
			Usage:   "total processing time for identity and account events (including setup, rules, and persisting)",
			Sources: cli.EnvVars("HEPA_IDENTITY_EVENT_TIMEOUT"),
			Value:   10 * time.Second,
		},
		&cli.DurationFlag{
			Name:    "ozone-event-timeout",
			Usage:   "total processing time for ozone events (including setup, rules, and persisting)",
			Sources: cli.EnvVars("HEPA_OZONE_EVENT_TIMEOUT"),
			Value:   30 * time.Second,
		},
	}

	app.Commands = []*cli.Command{
		runCmd,
		processRecordCmd,
		processRecentCmd,
		captureRecentCmd,
	}

	return app.Run(context.Background(), args)
}

func configDirectory(cmd *cli.Command) (identity.Directory, error) {
	baseDir := identity.BaseDirectory{
		PLCURL: cmd.String("atp-plc-host"),
		HTTPClient: http.Client{
			Timeout: time.Second * 15,
		},
		PLCLimiter:            rate.NewLimiter(rate.Limit(cmd.Int("plc-rate-limit")), 1),
		TryAuthoritativeDNS:   true,
		SkipDNSDomainSuffixes: []string{".bsky.social", ".staging.bsky.dev"},
	}
	var dir identity.Directory
	if cmd.String("redis-url") != "" {
		rdir, err := redisdir.NewRedisDirectory(&baseDir, cmd.String("redis-url"), time.Hour*24, time.Minute*2, time.Minute*5, 10_000)
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

func configLogger(cmd *cli.Command, writer io.Writer) *slog.Logger {
	var level slog.Level
	switch strings.ToLower(cmd.String("log-level")) {
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
			Sources: cli.EnvVars("HEPA_METRICS_LISTEN"),
		},
		&cli.StringFlag{
			Name: "slack-webhook-url",
			// eg: https://hooks.slack.com/services/X1234
			Usage:   "full URL of slack webhook",
			Sources: cli.EnvVars("SLACK_WEBHOOK_URL"),
		},
	},
	Action: func(ctx context.Context, cmd *cli.Command) error {
		logger := configLogger(cmd, os.Stdout)
		configOTEL("hepa")

		dir, err := configDirectory(cmd)
		if err != nil {
			return fmt.Errorf("failed to configure identity directory: %v", err)
		}

		srv, err := NewServer(
			dir,
			Config{
				Logger:               logger,
				BskyHost:             cmd.String("atp-bsky-host"),
				OzoneHost:            cmd.String("atp-ozone-host"),
				OzoneDID:             cmd.String("ozone-did"),
				OzoneAdminToken:      cmd.String("ozone-admin-token"),
				PDSHost:              cmd.String("atp-pds-host"),
				PDSAdminToken:        cmd.String("pds-admin-token"),
				SetsFileJSON:         cmd.String("sets-json-path"),
				RedisURL:             cmd.String("redis-url"),
				SlackWebhookURL:      cmd.String("slack-webhook-url"),
				HiveAPIToken:         cmd.String("hiveai-api-token"),
				AbyssHost:            cmd.String("abyss-host"),
				AbyssPassword:        cmd.String("abyss-password"),
				RatelimitBypass:      cmd.String("ratelimit-bypass"),
				RulesetName:          cmd.String("ruleset"),
				PreScreenHost:        cmd.String("prescreen-host"),
				PreScreenToken:       cmd.String("prescreen-token"),
				ReportDupePeriod:     cmd.Duration("report-dupe-period"),
				QuotaModReportDay:    cmd.Int("quota-mod-report-day"),
				QuotaModTakedownDay:  cmd.Int("quota-mod-takedown-day"),
				QuotaModActionDay:    cmd.Int("quota-mod-action-day"),
				RecordEventTimeout:   cmd.Duration("record-event-timeout"),
				IdentityEventTimeout: cmd.Duration("identity-event-timeout"),
				OzoneEventTimeout:    cmd.Duration("ozone-event-timeout"),
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
			if err := srv.RunMetrics(cmd.String("metrics-listen")); err != nil {
				slog.Error("failed to start metrics endpoint", "error", err)
				panic(fmt.Errorf("failed to start metrics endpoint: %w", err))
			}
		}()

		// firehose event consumer (note this is actually mandatory)
		relayHost := cmd.String("atp-relay-host")
		if relayHost != "" {
			fc := consumer.FirehoseConsumer{
				Engine:      srv.Engine,
				Logger:      logger.With("subsystem", "firehose-consumer"),
				Host:        cmd.String("atp-relay-host"),
				Parallelism: cmd.Int("firehose-parallelism"),
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
func configEphemeralServer(cmd *cli.Command) (*Server, error) {
	// NOTE: using stderr not stdout because some commands print to stdout
	logger := configLogger(cmd, os.Stderr)

	dir, err := configDirectory(cmd)
	if err != nil {
		return nil, err
	}

	return NewServer(
		dir,
		Config{
			Logger:          logger,
			BskyHost:        cmd.String("atp-bsky-host"),
			OzoneHost:       cmd.String("atp-ozone-host"),
			OzoneDID:        cmd.String("ozone-did"),
			OzoneAdminToken: cmd.String("ozone-admin-token"),
			PDSHost:         cmd.String("atp-pds-host"),
			PDSAdminToken:   cmd.String("pds-admin-token"),
			SetsFileJSON:    cmd.String("sets-json-path"),
			RedisURL:        cmd.String("redis-url"),
			HiveAPIToken:    cmd.String("hiveai-api-token"),
			AbyssHost:       cmd.String("abyss-host"),
			AbyssPassword:   cmd.String("abyss-password"),
			RatelimitBypass: cmd.String("ratelimit-bypass"),
			RulesetName:     cmd.String("ruleset"),
			PreScreenHost:   cmd.String("prescreen-host"),
			PreScreenToken:  cmd.String("prescreen-token"),
		},
	)
}

var processRecordCmd = &cli.Command{
	Name:      "process-record",
	Usage:     "process a single record in isolation",
	ArgsUsage: `<at-uri>`,
	Flags:     []cli.Flag{},
	Action: func(ctx context.Context, cmd *cli.Command) error {
		uriArg := cmd.Args().First()
		if uriArg == "" {
			return fmt.Errorf("expected a single AT-URI argument")
		}
		aturi, err := syntax.ParseATURI(uriArg)
		if err != nil {
			return fmt.Errorf("not a valid AT-URI: %v", err)
		}

		srv, err := configEphemeralServer(cmd)
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
	Action: func(ctx context.Context, cmd *cli.Command) error {
		idArg := cmd.Args().First()
		if idArg == "" {
			return fmt.Errorf("expected a single AT identifier (handle or DID) argument")
		}
		atid, err := syntax.ParseAtIdentifier(idArg)
		if err != nil {
			return fmt.Errorf("not a valid handle or DID: %v", err)
		}

		srv, err := configEphemeralServer(cmd)
		if err != nil {
			return err
		}

		return capture.FetchAndProcessRecent(ctx, srv.Engine, *atid, cmd.Int("limit"))
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
	Action: func(ctx context.Context, cmd *cli.Command) error {
		idArg := cmd.Args().First()
		if idArg == "" {
			return fmt.Errorf("expected a single AT identifier (handle or DID) argument")
		}
		atid, err := syntax.ParseAtIdentifier(idArg)
		if err != nil {
			return fmt.Errorf("not a valid handle or DID: %v", err)
		}

		srv, err := configEphemeralServer(cmd)
		if err != nil {
			return err
		}

		cap, err := capture.CaptureRecent(ctx, srv.Engine, *atid, cmd.Int("limit"))
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
