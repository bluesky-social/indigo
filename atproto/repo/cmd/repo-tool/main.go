package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"

	"github.com/bluesky-social/indigo/atproto/repo"

	"github.com/urfave/cli/v2"
)

func main() {
	app := cli.App{
		Name:  "repo-tool",
		Usage: "development tool for atproto MST trees, CAR files, etc",
	}
	app.Commands = []*cli.Command{
		&cli.Command{
			Name:      "verify-car-mst",
			Usage:     "load a CAR file and check the MST tree",
			ArgsUsage: "<path>",
			Action:    runVerifyCarMst,
		},
	}
	h := slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelDebug})
	slog.SetDefault(slog.New(h))
	app.RunAndExitOnError()
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
