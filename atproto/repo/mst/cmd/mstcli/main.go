package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"

	"github.com/bluesky-social/indigo/atproto/repo/mst"

	"github.com/urfave/cli/v2"
)

func main() {
	app := cli.App{
		Name:  "mstcli",
		Usage: "development tool for verifying MST implementation",
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

	tree, rootCID, err := mst.ReadTreeFromCar(ctx, f)
	if err != nil {
		return err
	}

	computedCID, err := mst.NodeCID(tree)
	if err != nil {
		return err
	}

	if *rootCID != *computedCID {
		return fmt.Errorf("failed to re-compute: %s != %s", computedCID, rootCID)
	}
	fmt.Println("verified tree")
	return nil
}
