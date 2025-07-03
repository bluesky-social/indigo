package main

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	comatproto "github.com/gander-social/gander-indigo-sovereign/api/atproto"
	"github.com/gander-social/gander-indigo-sovereign/xrpc"

	"github.com/urfave/cli/v2"
)

var cmdPds = &cli.Command{
	Name:  "pds",
	Usage: "sub-commands for pds hosts",
	Flags: []cli.Flag{},
	Subcommands: []*cli.Command{
		&cli.Command{
			Name:      "describe",
			Usage:     "shows info about a PDS info",
			ArgsUsage: `<url>`,
			Action:    runPdsDescribe,
		},
	},
}

func runPdsDescribe(cctx *cli.Context) error {
	ctx := context.Background()

	pdsHost := cctx.Args().First()
	if pdsHost == "" {
		return fmt.Errorf("need to provide new handle as argument")
	}
	if !strings.Contains(pdsHost, "://") {
		return fmt.Errorf("PDS host is not a url: %s", pdsHost)
	}
	client := xrpc.Client{
		Host:      pdsHost,
		UserAgent: userAgent(),
	}

	resp, err := comatproto.ServerDescribeServer(ctx, &client)
	if err != nil {
		return err
	}

	b, err := json.MarshalIndent(resp, "", "  ")
	if err != nil {
		return err
	}
	fmt.Println(string(b))

	return nil
}
