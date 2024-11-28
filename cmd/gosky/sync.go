package main

import (
	"fmt"
	"os"

	cli "github.com/urfave/cli/v2"

	"github.com/bluesky-social/indigo/api/atproto"
	"github.com/bluesky-social/indigo/atproto/identity"
	"github.com/bluesky-social/indigo/atproto/syntax"
	"github.com/bluesky-social/indigo/util/cliutil"
)

var syncCmd = &cli.Command{
	Name:  "sync",
	Usage: "sub-commands for repo sync endpoints",
	Subcommands: []*cli.Command{
		syncGetRepoCmd,
		syncGetRootCmd,
		syncListReposCmd,
	},
}

var syncGetRepoCmd = &cli.Command{
	Name:      "get-repo",
	Usage:     "download repo from account's PDS to local file (or '-' for stdout). for hex combine with 'xxd -ps -u -c 0'",
	ArgsUsage: `<at-identifier> [<car-file-path>]`,
	Flags: []cli.Flag{
		&cli.StringFlag{
			Name: "host",
		},
	},
	Action: func(cctx *cli.Context) error {
		ctx := cctx.Context
		arg := cctx.Args().First()
		if arg == "" {
			return fmt.Errorf("at-identifier arg is required")
		}
		atid, err := syntax.ParseAtIdentifier(arg)
		if err != nil {
			return err
		}
		dir := identity.DefaultDirectory()
		ident, err := dir.Lookup(ctx, *atid)
		if err != nil {
			return err
		}

		carPath := cctx.Args().Get(1)
		if carPath == "" {
			carPath = ident.DID.String() + ".car"
		}

		xrpcc, err := cliutil.GetXrpcClient(cctx, false)
		if err != nil {
			return err
		}
		xrpcc.Host = ident.PDSEndpoint()
		if xrpcc.Host == "" {
			return fmt.Errorf("no PDS endpoint for identity")
		}

		if h := cctx.String("host"); h != "" {
			xrpcc.Host = h
		}

		log.Infof("downloading from %s to: %s", xrpcc.Host, carPath)
		repoBytes, err := atproto.SyncGetRepo(ctx, xrpcc, ident.DID.String(), "")
		if err != nil {
			return err
		}

		if carPath == "-" {
			_, err = os.Stdout.Write(repoBytes)
			return err
		} else {
			return os.WriteFile(carPath, repoBytes, 0666)
		}
	},
}

var syncGetRootCmd = &cli.Command{
	Name:      "get-root",
	ArgsUsage: `<did>`,
	Action: func(cctx *cli.Context) error {
		xrpcc, err := cliutil.GetXrpcClient(cctx, false)
		if err != nil {
			return err
		}

		ctx := cctx.Context

		atid, err := syntax.ParseAtIdentifier(cctx.Args().First())
		if err != nil {
			return err
		}

		dir := identity.DefaultDirectory()
		ident, err := dir.Lookup(ctx, *atid)
		if err != nil {
			return err
		}

		xrpcc.Host = ident.PDSEndpoint()
		if xrpcc.Host == "" {
			return fmt.Errorf("no PDS endpoint for identity")
		}

		root, err := atproto.SyncGetHead(ctx, xrpcc, cctx.Args().First())
		if err != nil {
			return err
		}

		fmt.Println(root.Root)

		return nil
	},
}

var syncListReposCmd = &cli.Command{
	Name: "list-repos",
	Action: func(cctx *cli.Context) error {
		xrpcc, err := cliutil.GetXrpcClient(cctx, false)
		if err != nil {
			return err
		}

		var curs string
		for {
			out, err := atproto.SyncListRepos(cctx.Context, xrpcc, curs, 1000)
			if err != nil {
				return err
			}

			if len(out.Repos) == 0 {
				break
			}

			for _, r := range out.Repos {
				fmt.Println(r.Did)
			}

			if out.Cursor == nil {
				break
			}

			curs = *out.Cursor
		}

		return nil
	},
}
