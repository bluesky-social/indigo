package main

import (
	"log/slog"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/bluesky-social/indigo/atproto/syntax"
	"github.com/hashicorp/golang-lru/v2/expirable"
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
		Name:  "handlr",
		Usage: "atproto handle DNS TXT proxy demon",
	}

	flags := []cli.Flag{
		&cli.StringFlag{
			Name:    "bind",
			Usage:   "local UDP IP and port to listen on. note that DNS port 53 requires superuser on most systems",
			Value:   ":5333",
			EnvVars: []string{"HANDLR_BIND"},
		},
		&cli.StringFlag{
			Name:    "backend-host",
			Usage:   "HTTP method, hostname, and port of backend resolution service",
			Value:   "http://localhost:5000",
			EnvVars: []string{"HANDLR_BACKEND_HOST"},
		},
		&cli.StringFlag{
			Name:    "domain-suffix",
			Usage:   "domain suffix to filter against",
			EnvVars: []string{"HANDLR_DOMAIN_SUFFIX"},
		},
		&cli.IntFlag{
			Name:    "ttl",
			Usage:   "TTL for both DNS TXT responses, and internal caching",
			Value:   5 * 60,
			EnvVars: []string{"HANDLR_TTL"},
		},
		&cli.StringFlag{
			Name:    "log-level",
			Usage:   "log level (debug, info, warn, error)",
			Value:   "info",
			EnvVars: []string{"HANDLR_LOG_LEVEL", "LOG_LEVEL"},
		},
	}
	app.Commands = []*cli.Command{
		&cli.Command{
			Name:   "serve",
			Usage:  "run the handlr daemon",
			Action: runServe,
			Flags:  flags,
		},
	}
	return app.Run(args)
}

func runServe(cctx *cli.Context) error {

	logLevel := slog.LevelInfo
	switch strings.ToLower(cctx.String("log-level")) {
	case "debug":
		logLevel = slog.LevelDebug
	case "info":
		logLevel = slog.LevelInfo
	case "warn":
		logLevel = slog.LevelWarn
	case "error":
		logLevel = slog.LevelError
	}
	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{
		Level:     logLevel,
		AddSource: true,
	}))
	slog.SetDefault(logger)

	// set a minimum for the internal cache
	var cache *expirable.LRU[syntax.Handle, syntax.DID]
	ttl := cctx.Int("ttl")
	if ttl != 0 {
		cache = expirable.NewLRU[syntax.Handle, syntax.DID](10_000, nil, time.Second*time.Duration(ttl))
	}
	client := http.Client{Timeout: time.Second * 5}
	hr := HTTPResolver{
		client:      &client,
		backendHost: cctx.String("backend-host"),
		suffix:      cctx.String("domain-suffix"),
		ttl:         cctx.Int("ttl"),
		cache:       cache,
	}
	return hr.Run(cctx.String("bind"))
}
