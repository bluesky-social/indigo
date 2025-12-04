package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"

	"github.com/bluesky-social/indigo/atproto/syntax"

	"github.com/urfave/cli/v3"
)

func main() {
	app := cli.Command{
		Name:  "atp-syntax",
		Usage: "informal debugging CLI tool for atproto syntax (identifiers)",
	}
	app.Commands = []*cli.Command{
		&cli.Command{
			Name:      "parse-tid",
			Usage:     "parse a TID and output timestamp",
			ArgsUsage: "<tid>",
			Action:    runParseTID,
		},
		&cli.Command{
			Name:      "parse-did",
			Usage:     "parse a DID",
			ArgsUsage: "<did>",
			Action:    runParseDID,
		},
	}
	h := slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelDebug})
	slog.SetDefault(slog.New(h))
	if err := app.Run(context.Background(), os.Args); err != nil {
		slog.Error("command failed", "error", err)
		os.Exit(-1)
	}
}

func runParseTID(ctx context.Context, cmd *cli.Command) error {
	s := cmd.Args().First()
	if s == "" {
		return fmt.Errorf("need to provide identifier as an argument")
	}

	tid, err := syntax.ParseTID(s)
	if err != nil {
		return err
	}
	fmt.Printf("TID: %s\n", tid)
	fmt.Printf("Time: %s\n", tid.Time())

	return nil
}

func runParseDID(ctx context.Context, cmd *cli.Command) error {
	s := cmd.Args().First()
	if s == "" {
		return fmt.Errorf("need to provide identifier as an argument")
	}

	did, err := syntax.ParseDID(s)
	if err != nil {
		return err
	}
	fmt.Printf("%s\n", did)

	return nil
}
