package main

import (
	"context"
	"fmt"

	comatproto "github.com/bluesky-social/indigo/api/atproto"
	"github.com/bluesky-social/indigo/atproto/identity"
	"github.com/bluesky-social/indigo/atproto/syntax"
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

		dir := identity.DefaultDirectory()
		resp, err := dir.LookupHandle(ctx, syntax.Handle(handle))
		if err != nil {
			return fmt.Errorf("resolving %q: %w", handle, err)
		}

		fmt.Println(resp.DID)

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
