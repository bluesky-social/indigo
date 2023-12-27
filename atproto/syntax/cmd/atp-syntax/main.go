package main

import (
	"fmt"
	"log/slog"
	"os"

	"github.com/bluesky-social/indigo/atproto/syntax"

	"github.com/urfave/cli/v2"
)

func main() {
	app := cli.App{
		Name:  "atp-syntax",
		Usage: "informal debugging CLI tool for atproto syntax (identifiers)",
	}
	app.Commands = []*cli.Command{
		{
			Name:      "parse-tid",
			Usage:     "parse a TID and output timestamp",
			ArgsUsage: "<tid>",
			Action:    runParseTID,
		},
	}
	h := slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelDebug})
	slog.SetDefault(slog.New(h))
	app.RunAndExitOnError()
}

func runParseTID(cctx *cli.Context) error {
	s := cctx.Args().First()
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
