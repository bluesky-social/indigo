package main

import (
	"fmt"

	"github.com/urfave/cli/v2"

	"github.com/bluesky-social/indigo/api/atproto"
	"github.com/bluesky-social/indigo/atproto/identity"
	"github.com/bluesky-social/indigo/atproto/syntax"
	"github.com/bluesky-social/indigo/util/cliutil"
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
		ctx := cctx.Context

		args, err := needArgs(cctx, "handle")
		if err != nil {
			return err
		}

		h, err := syntax.ParseHandle(args[0])
		if err != nil {
			return fmt.Errorf("resolving %q: %w", args[0], err)
		}

		dir := identity.DefaultDirectory()

		res, err := dir.LookupHandle(ctx, h)
		if err != nil {
			return err
		}

		fmt.Println(res.DID)

		return nil
	},
}

var updateHandleCmd = &cli.Command{
	Name:      "update",
	ArgsUsage: `<handle>`,
	Action: func(cctx *cli.Context) error {
		ctx := cctx.Context

		xrpcc, err := cliutil.GetXrpcClient(cctx, false)
		if err != nil {
			return err
		}

		args, err := needArgs(cctx, "handle")
		if err != nil {
			return err
		}
		handle := args[0]

		err = atproto.IdentityUpdateHandle(ctx, xrpcc, &atproto.IdentityUpdateHandle_Input{
			Handle: handle,
		})
		if err != nil {
			return err
		}

		return nil
	},
}
