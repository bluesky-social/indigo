package main

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"os"
	"strings"

	"github.com/bluesky-social/indigo/atproto/repo"

	"github.com/urfave/cli/v2"
)

func main() {
	app := cli.App{
		Name:  "repo-tool",
		Usage: "development tool for atproto MST trees, CAR files, etc",
		Flags: []cli.Flag{
			&cli.StringFlag{
				Name:    "log-level",
				Usage:   "log verbosity level (eg: warn, info, debug)",
				EnvVars: []string{"BEEMO_LOG_LEVEL", "GO_LOG_LEVEL", "LOG_LEVEL"},
			},
		},
	}
	app.Commands = []*cli.Command{
		&cli.Command{
			Name:      "verify-car-mst",
			Usage:     "load a CAR file and check the MST tree",
			ArgsUsage: "<path>",
			Action:    runVerifyCarMst,
		},
		&cli.Command{
			Name:   "verify-firehose",
			Usage:  "subscribes to sync firehose and validates commit messages",
			Action: runVerifyFirehose,
			Flags: []cli.Flag{
				&cli.StringFlag{
					Name:    "relay-host",
					Usage:   "method, hostname, and port of Relay instance (websocket)",
					Value:   "wss://bsky.network",
					EnvVars: []string{"ATP_RELAY_HOST"},
				},
			},
		},
	}
	h := slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelDebug})
	slog.SetDefault(slog.New(h))
	app.RunAndExitOnError()
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

func runVerifyCarMst(cctx *cli.Context) error {
	ctx := context.Background()
	p := cctx.Args().First()
	if p == "" {
		return fmt.Errorf("need to provide path to CAR file")
	}

	f, err := os.Open(p)
	if err != nil {
		return err
	}
	defer f.Close()

	repo, err := repo.LoadFromCAR(ctx, f)
	if err != nil {
		return err
	}

	computedCID, err := repo.MST.RootCID()
	if err != nil {
		return err
	}

	if repo.Commit.Data != *computedCID {
		return fmt.Errorf("failed to re-compute: %s != %s", computedCID, repo.Commit.Data)
	}
	fmt.Println("verified tree")
	return nil
}
