package main

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	comatproto "github.com/bluesky-social/indigo/api/atproto"
	"github.com/bluesky-social/indigo/xrpc"

	"github.com/urfave/cli/v3"
)

var cmdPds = &cli.Command{
	Name:  "pds",
	Usage: "sub-commands for pds hosts",
	Flags: []cli.Flag{},
	Commands: []*cli.Command{
		&cli.Command{
			Name:      "describe",
			Usage:     "shows info about a PDS info",
			ArgsUsage: `<url>`,
			Action:    runPdsDescribe,
		},
	},
}

func runPdsDescribe(ctx context.Context, cmd *cli.Command) error {

	pdsHost := cmd.Args().First()
	if pdsHost == "" {
		return fmt.Errorf("need to provide new handle as argument")
	}
	if !strings.Contains(pdsHost, "://") {
		return fmt.Errorf("PDS host is not a url: %s", pdsHost)
	}
	client := xrpc.Client{
		Host: pdsHost,
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
