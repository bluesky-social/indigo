package main

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"time"

	"github.com/bluesky-social/indigo/atproto/identity"
	"github.com/bluesky-social/indigo/automod"

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
		&cli.StringFlag{
			Name:    "atp-mod-host",
			Usage:   "method, hostname, and port of moderation service",
			Value:   "https://api.bsky.app",
			EnvVars: []string{"ATP_MOD_HOST"},
		},
		&cli.StringFlag{
			Name:    "atp-bsky-host",
			Usage:   "method, hostname, and port of bsky API (appview) service",
			Value:   "https://api.bsky.app",
			EnvVars: []string{"ATP_BSKY_HOST"},
		},
	}

	app.Commands = []*cli.Command{
		runCmd,
	}

	return app.Run(args)
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
		&cli.IntFlag{
			Name:    "plc-rate-limit",
			Usage:   "max number of requests per second to PLC registry",
			Value:   100,
			EnvVars: []string{"HEPA_PLC_RATE_LIMIT"},
		},
		&cli.StringFlag{
			Name:    "mod-handle",
			Usage:   "for mod service login",
			EnvVars: []string{"HEPA_MOD_AUTH_HANDLE"},
		},
		&cli.StringFlag{
			Name:    "mod-password",
			Usage:   "for mod service login",
			EnvVars: []string{"HEPA_MOD_AUTH_PASSWORD"},
		},
		&cli.StringFlag{
			Name:    "mod-admin-token",
			Usage:   "admin authentication password for mod service",
			EnvVars: []string{"HEPA_MOD_AUTH_ADMIN_TOKEN"},
		},
		&cli.StringFlag{
			Name:    "sets-json-path",
			Usage:   "file path of JSON file containing static sets",
			EnvVars: []string{"HEPA_SETS_JSON_PATH"},
		},
		&cli.StringFlag{
			Name:  "redis-url",
			Usage: "redis connection URL",
			// redis://<user>:<pass>@localhost:6379/<db>
			// redis://localhost:6379/0
			EnvVars: []string{"HEPA_REDIS_URL"},
		},
	},
	Action: func(cctx *cli.Context) error {
		ctx := context.Background()
		logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{
			Level: slog.LevelInfo,
		}))
		slog.SetDefault(logger)

		configOTEL("hepa")

		baseDir := identity.BaseDirectory{
			PLCURL: cctx.String("atp-plc-host"),
			HTTPClient: http.Client{
				Timeout: time.Second * 15,
			},
			PLCLimiter:            rate.NewLimiter(rate.Limit(cctx.Int("plc-rate-limit")), 1),
			TryAuthoritativeDNS:   true,
			SkipDNSDomainSuffixes: []string{".bsky.social"},
		}
		var dir identity.Directory
		if cctx.String("redis-url") != "" {
			rdir, err := automod.NewRedisDirectory(&baseDir, cctx.String("redis-url"), time.Hour*24, time.Minute*2)
			if err != nil {
				return err
			}
			dir = rdir
		} else {
			cdir := identity.NewCacheDirectory(&baseDir, 1_500_000, time.Hour*24, time.Minute*2)
			dir = &cdir
		}

		srv, err := NewServer(
			dir,
			Config{
				BGSHost:       cctx.String("atp-bgs-host"),
				BskyHost:      cctx.String("atp-bsky-host"),
				Logger:        logger,
				ModHost:       cctx.String("atp-mod-host"),
				ModAdminToken: cctx.String("mod-admin-token"),
				ModUsername:   cctx.String("mod-handle"),
				ModPassword:   cctx.String("mod-password"),
				SetsFileJSON:  cctx.String("sets-json-path"),
				RedisURL:      cctx.String("redis-url"),
			},
		)
		if err != nil {
			return err
		}

		// prometheus HTTP endpoint: /metrics
		go func() {
			if err := srv.RunMetrics(cctx.String("metrics-listen")); err != nil {
				slog.Error("failed to start metrics endpoint", "error", err)
				panic(fmt.Errorf("failed to start metrics endpoint: %w", err))
			}
		}()

		// the main service loop
		if err := srv.RunConsumer(ctx); err != nil {
			return fmt.Errorf("failure consuming and processing firehose: %w", err)
		}
		return nil
	},
}
