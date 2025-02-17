package main

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"os"
	"strings"

	"github.com/bluesky-social/indigo/atproto/identity"
	"github.com/bluesky-social/indigo/atproto/repo"
	"github.com/bluesky-social/indigo/atproto/syntax"

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
			Name:      "verify-car-signature",
			Usage:     "load a CAR file and check the commit message signature",
			ArgsUsage: "<path>",
			Action:    runVerifyCarSignature,
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

	commit, repo, err := repo.LoadFromCAR(ctx, f)
	if err != nil {
		return err
	}

	computedCID, err := repo.MST.RootCID()
	if err != nil {
		return err
	}

	if commit.Data != *computedCID {
		return fmt.Errorf("failed to re-compute: %s != %s", computedCID, commit.Data)
	}
	fmt.Println("verified tree")
	return nil
}

func runVerifyCarSignature(cctx *cli.Context) error {
	ctx := context.Background()
	dir := identity.DefaultDirectory()

	p := cctx.Args().First()
	if p == "" {
		return fmt.Errorf("need to provide path to CAR file")
	}

	f, err := os.Open(p)
	if err != nil {
		return err
	}
	defer f.Close()

	commit, _, err := repo.LoadFromCAR(ctx, f)
	if err != nil {
		return err
	}

	if err := commit.VerifyStructure(); err != nil {
		return err
	}
	did, err := syntax.ParseDID(commit.DID)
	if err != nil {
		return err
	}

	ident, err := dir.LookupDID(ctx, did)
	if err != nil {
		return err
	}
	pubkey, err := ident.PublicKey()
	if err != nil {
		return err
	}
	if err := commit.VerifySignature(pubkey); err != nil {
		return err
	}
	fmt.Println("verified signature")
	return nil
}
