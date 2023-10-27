package main

import (
	"context"
	"fmt"

	api "github.com/bluesky-social/indigo/api"
	comatproto "github.com/bluesky-social/indigo/api/atproto"
	"github.com/bluesky-social/indigo/util/cliutil"

	cli "github.com/urfave/cli/v2"
)

var handleCmd = &cli.Command{
	Name:  "handle",
	Usage: "sub-commands for working handles",
	Subcommands: []*cli.Command{
		resolveHandleCmd,
		updateHandleCmd,
	},
}

var resolveHandleCmd = &cli.Command{
	Name:      "resolve",
	ArgsUsage: `<handle>`,
	Action: func(cctx *cli.Context) error {
		ctx := context.TODO()

		args, err := needArgs(cctx, "handle")
		if err != nil {
			return err
		}
		handle := args[0]

		phr := &api.ProdHandleResolver{}
		out, err := phr.ResolveHandleToDid(ctx, handle)
		if err != nil {
			return err
		}

		fmt.Println(out)

		return nil
	},
}

var updateHandleCmd = &cli.Command{
	Name:      "update",
	ArgsUsage: `<handle>`,
	Action: func(cctx *cli.Context) error {
		ctx := context.TODO()

		xrpcc, err := cliutil.GetXrpcClient(cctx, false)
		if err != nil {
			return err
		}

		args, err := needArgs(cctx, "handle")
		if err != nil {
			return err
		}
		handle := args[0]

		err = comatproto.IdentityUpdateHandle(ctx, xrpcc, &comatproto.IdentityUpdateHandle_Input{
			Handle: handle,
		})
		if err != nil {
			return err
		}

		return nil
	},
}
