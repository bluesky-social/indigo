package main

import (
	"fmt"
	"io"
	"log/slog"
	_ "net/http/pprof"
	"os"
	"runtime"
	"strings"

	"github.com/carlmjohnson/versioninfo"
	_ "github.com/joho/godotenv/autoload"
	cli "github.com/urfave/cli/v2"
)

func main() {
	if err := run(os.Args); err != nil {
		slog.Error("exiting", "err", err)
		os.Exit(-1)
	}
}

func run(args []string) error {

	app := cli.App{
		Name:    "domesday",
		Usage:   "atproto identity directory",
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
		&cli.IntFlag{
			Name:    "plc-rate-limit",
			Usage:   "max number of requests per second to PLC registry",
			Value:   100,
			EnvVars: []string{"DOMESDAY_PLC_RATE_LIMIT"},
		},
		&cli.StringFlag{
			Name:    "redis-url",
			Usage:   "redis connection URL: redis://<user>:<pass>@<hostname>:6379/<db>",
			Value:   "redis://localhost:6379/0",
			EnvVars: []string{"DOMESDAY_REDIS_URL"},
		},
		&cli.StringFlag{
			Name:    "log-level",
			Usage:   "log verbosity level (eg: warn, info, debug)",
			EnvVars: []string{"DOMESDAY_LOG_LEVEL", "GO_LOG_LEVEL", "LOG_LEVEL"},
		},
	}

	app.Commands = []*cli.Command{
		serveCmd,
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

var serveCmd = &cli.Command{
	Name:  "serve",
	Usage: "run the domesday API daemon",
	Flags: []cli.Flag{
		&cli.StringFlag{
			Name:     "bind",
			Usage:    "Specify the local IP/port to bind to",
			Required: false,
			Value:    ":6600",
			EnvVars:  []string{"DOMESDAY_BIND"},
		},
		&cli.StringFlag{
			Name:    "metrics-listen",
			Usage:   "IP or address, and port, to listen on for metrics APIs",
			Value:   ":3989",
			EnvVars: []string{"DOMESDAY_METRICS_LISTEN"},
		},
	},
	Action: func(cctx *cli.Context) error {
		logger := configLogger(cctx, os.Stdout)
		//configOTEL("domesday")

		srv, err := NewServer(
			Config{
				Logger:       logger,
				FirehoseHost: cctx.String("atp-relay-host"),
				RedisURL:     cctx.String("redis-url"),
				Bind:         cctx.String("bind"),
			},
		)
		if err != nil {
			return fmt.Errorf("failed to construct server: %v", err)
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

		return srv.RunAPI()
	},
}
