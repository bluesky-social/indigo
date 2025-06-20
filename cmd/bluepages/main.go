package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	_ "net/http/pprof"
	"os"
	"runtime"
	"strings"

	"github.com/gander-social/gander-indigo-sovereign/atproto/identity/apidir"
	"github.com/gander-social/gander-indigo-sovereign/atproto/syntax"

	"github.com/carlmjohnson/versioninfo"
	_ "github.com/joho/godotenv/autoload"
	"github.com/urfave/cli/v2"
)

func main() {
	if err := run(os.Args); err != nil {
		slog.Error("exiting", "err", err)
		os.Exit(-1)
	}
}

func run(args []string) error {

	app := cli.App{
		Name:    "bluepages",
		Usage:   "atproto identity directory",
		Version: versioninfo.Short(),
		Flags: []cli.Flag{
			&cli.StringFlag{
				Name:    "atp-relay-host",
				Usage:   "hostname and port of Relay to subscribe to",
				Value:   "wss://gndr.network",
				EnvVars: []string{"ATP_RELAY_HOST", "ATP_BGS_HOST"},
			},
			&cli.StringFlag{
				Name:    "atp-plc-host",
				Usage:   "method, hostname, and port of PLC registry",
				Value:   "https://plc.directory",
				EnvVars: []string{"ATP_PLC_HOST"},
			},
			&cli.IntFlag{
				Name:    "plc-rate-limit",
				Usage:   "max number of requests per second to PLC registry",
				Value:   300,
				EnvVars: []string{"BLUEPAGES_PLC_RATE_LIMIT"},
			},
			&cli.StringFlag{
				Name:    "redis-url",
				Usage:   "redis connection URL: redis://<user>:<pass>@<hostname>:6379/<db>",
				Value:   "redis://localhost:6379/0",
				EnvVars: []string{"BLUEPAGES_REDIS_URL"},
			},
			&cli.StringFlag{
				Name:    "log-level",
				Usage:   "log verbosity level (eg: warn, info, debug)",
				EnvVars: []string{"BLUEPAGES_LOG_LEVEL", "GO_LOG_LEVEL", "LOG_LEVEL"},
			},
		},
		Commands: []*cli.Command{
			&cli.Command{
				Name:   "serve",
				Usage:  "run the bluepages API daemon",
				Action: runServeCmd,
				Flags: []cli.Flag{
					&cli.StringFlag{
						Name:     "bind",
						Usage:    "Specify the local IP/port to bind to",
						Required: false,
						Value:    ":6600",
						EnvVars:  []string{"BLUEPAGES_BIND"},
					},
					&cli.StringFlag{
						Name:    "metrics-listen",
						Usage:   "IP or address, and port, to listen on for metrics APIs",
						Value:   ":3989",
						EnvVars: []string{"BLUEPAGES_METRICS_LISTEN"},
					},
					&cli.BoolFlag{
						Name:    "disable-firehose-consumer",
						Usage:   "don't consume #identity events from firehose",
						EnvVars: []string{"BLUEPAGES_DISABLE_FIREHOSE_CONSUMER"},
					},
					&cli.BoolFlag{
						Name:    "disable-refresh",
						Usage:   "disable the refreshIdentity API endpoint",
						EnvVars: []string{"BLUEPAGES_DISABLE_REFRESH"},
					},
					&cli.IntFlag{
						Name:    "firehose-parallelism",
						Usage:   "number of concurrent firehose workers",
						Value:   4,
						EnvVars: []string{"BLUEPAGES_FIREHOSE_PARALLELISM"},
					},
				},
			},
			&cli.Command{
				Name:      "resolve-handle",
				ArgsUsage: `<handle>`,
				Usage:     "query service for handle resoltion",
				Action:    runResolveHandleCmd,
				Flags: []cli.Flag{
					&cli.StringFlag{
						Name:    "host",
						Usage:   "bluepages server to send request to",
						Value:   "http://localhost:6600",
						EnvVars: []string{"BLUEPAGES_HOST"},
					},
				},
			},
			&cli.Command{
				Name:      "resolve-did",
				ArgsUsage: `<did>`,
				Usage:     "query service for DID document resoltion",
				Action:    runResolveDIDCmd,
				Flags: []cli.Flag{
					&cli.StringFlag{
						Name:    "host",
						Usage:   "bluepages server to send request to",
						Value:   "http://localhost:6600",
						EnvVars: []string{"BLUEPAGES_HOST"},
					},
				},
			},
			&cli.Command{
				Name:      "lookup",
				ArgsUsage: `<at-identifier>`,
				Usage:     "query service for identity resoltion",
				Action:    runLookupCmd,
				Flags: []cli.Flag{
					&cli.StringFlag{
						Name:    "host",
						Usage:   "bluepages server to send request to",
						Value:   "http://localhost:6600",
						EnvVars: []string{"BLUEPAGES_HOST"},
					},
				},
			},
			&cli.Command{
				Name:      "refresh",
				ArgsUsage: `<at-identifier>`,
				Usage:     "ask service to refresh identity",
				Action:    runRefreshCmd,
				Flags: []cli.Flag{
					&cli.StringFlag{
						Name:    "host",
						Usage:   "bluepages server to send request to",
						Value:   "http://localhost:6600",
						EnvVars: []string{"BLUEPAGES_HOST"},
					},
				},
			},
		},
	}

	return app.Run(args)
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

func configClient(cctx *cli.Context) apidir.APIDirectory {
	return apidir.NewAPIDirectory(cctx.String("host"))
}

func runServeCmd(cctx *cli.Context) error {
	logger := configLogger(cctx, os.Stdout)
	ctx := context.Background()

	srv, err := NewServer(
		Config{
			Logger:         logger,
			Bind:           cctx.String("bind"),
			RedisURL:       cctx.String("redis-url"),
			PLCHost:        cctx.String("atp-plc-host"),
			PLCRateLimit:   cctx.Int("plc-rate-limit"),
			DisableRefresh: cctx.Bool("disable-refresh"),
		},
	)
	if err != nil {
		return fmt.Errorf("failed to construct server: %v", err)
	}

	if !cctx.Bool("disable-firehose-consumer") {
		go func() {
			firehoseHost := cctx.String("atp-relay-host")
			firehoseParallelism := cctx.Int("firehose-parallelism")
			if err := srv.RunFirehoseConsumer(ctx, firehoseHost, firehoseParallelism); err != nil {
				slog.Error("firehose consumer thread failed", "err", err)
				// NOTE: not crashing or halting process here
			}
		}()
		go func() {
			if err := srv.RunPersistCursor(ctx); err != nil {
				slog.Error("firehose persist thread failed", "err", err)
				// NOTE: not crashing or halting process here
			}
		}()
	}

	// prometheus HTTP endpoint: /metrics
	go func() {
		// TODO: what is this tuning for? just cargo-culted it
		runtime.SetBlockProfileRate(10)
		runtime.SetMutexProfileFraction(10)
		if err := srv.RunMetrics(cctx.String("metrics-listen")); err != nil {
			slog.Error("failed to start metrics endpoint", "error", err)
			// NOTE: not crashing or halting process here
		}
	}()

	return srv.RunAPI()
}

func runResolveHandleCmd(cctx *cli.Context) error {
	ctx := context.Background()
	dir := configClient(cctx)

	s := cctx.Args().First()
	if s == "" {
		return fmt.Errorf("need to provide identifier for resolution")
	}
	handle, err := syntax.ParseHandle(s)
	if err != nil {
		return err
	}

	did, err := dir.ResolveHandle(ctx, handle)
	if err != nil {
		return err
	}
	fmt.Println(did.String())
	return nil
}

func runResolveDIDCmd(cctx *cli.Context) error {
	ctx := context.Background()
	dir := configClient(cctx)

	s := cctx.Args().First()
	if s == "" {
		return fmt.Errorf("need to provide identifier for resolution")
	}
	did, err := syntax.ParseDID(s)
	if err != nil {
		return err
	}

	raw, err := dir.ResolveDIDRaw(ctx, did)
	if err != nil {
		return err
	}
	b, err := json.MarshalIndent(raw, "", "  ")
	if err != nil {
		return err
	}
	fmt.Println(string(b))
	return nil
}

func runLookupCmd(cctx *cli.Context) error {
	ctx := context.Background()
	dir := configClient(cctx)

	s := cctx.Args().First()
	if s == "" {
		return fmt.Errorf("need to provide identifier for resolution")
	}
	atid, err := syntax.ParseAtIdentifier(s)
	if err != nil {
		return err
	}

	ident, err := dir.Lookup(ctx, *atid)
	if err != nil {
		return err
	}

	b, err := json.MarshalIndent(ident, "", "  ")
	if err != nil {
		return err
	}
	fmt.Println(string(b))
	return nil
}

func runRefreshCmd(cctx *cli.Context) error {
	ctx := context.Background()
	dir := configClient(cctx)

	s := cctx.Args().First()
	if s == "" {
		return fmt.Errorf("need to provide identifier for resolution")
	}
	atid, err := syntax.ParseAtIdentifier(s)
	if err != nil {
		return err
	}

	err = dir.Purge(ctx, *atid)
	if err != nil {
		return err
	}

	ident, err := dir.Lookup(ctx, *atid)
	if err != nil {
		return err
	}

	b, err := json.MarshalIndent(ident, "", "  ")
	if err != nil {
		return err
	}
	fmt.Println(string(b))
	return nil
}
