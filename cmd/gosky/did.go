package main

import (
	"encoding/json"
	"fmt"

	"github.com/urfave/cli/v2"

	"github.com/bluesky-social/indigo/atproto/identity"
	"github.com/bluesky-social/indigo/atproto/syntax"
	"github.com/bluesky-social/indigo/util/cliutil"
)

var didCmd = &cli.Command{
	Name:  "did",
	Usage: "sub-commands for working with DIDs",
	Flags: []cli.Flag{},
	Subcommands: []*cli.Command{
		didGetCmd,
		didCreateCmd,
		didKeyCmd,
	},
}

var didGetCmd = &cli.Command{
	Name:      "get",
	ArgsUsage: `<did>`,
	Flags: []cli.Flag{
		&cli.BoolFlag{
			Name:  "handle",
			Usage: "resolve did to handle and print",
		},
	},
	Action: func(cctx *cli.Context) error {
		s := cliutil.GetDidResolver(cctx)

		ctx := cctx.Context
		did := cctx.Args().First()

		dir := identity.DefaultDirectory()

		if cctx.Bool("handle") {
			id, err := dir.LookupDID(ctx, syntax.DID(did))
			if err != nil {
				return err
			}

			fmt.Println(id.Handle)
			return nil
		}

		doc, err := s.GetDocument(cctx.Context, did)
		if err != nil {
			return err
		}

		b, err := json.MarshalIndent(doc, "", "  ")
		if err != nil {
			return err
		}

		fmt.Println(string(b))
		return nil
	},
}

var didCreateCmd = &cli.Command{
	Name:      "create",
	ArgsUsage: `<handle> <service>`,
	Flags: []cli.Flag{
		&cli.StringFlag{
			Name: "recoverydid",
		},
		&cli.StringFlag{
			Name: "signingkey",
		},
	},
	Action: func(cctx *cli.Context) error {
		s := cliutil.GetPLCClient(cctx)

		args, err := needArgs(cctx, "handle", "service")
		if err != nil {
			return err
		}
		handle, service := args[0], args[1]

		recoverydid := cctx.String("recoverydid")

		sigkey, err := cliutil.LoadKeyFromFile(cctx.String("signingkey"))
		if err != nil {
			return err
		}

		fmt.Println("KEYDID: ", sigkey.Public().DID())

		ndid, err := s.CreateDID(cctx.Context, sigkey, recoverydid, handle, service)
		if err != nil {
			return err
		}

		fmt.Println(ndid)
		return nil
	},
}

var didKeyCmd = &cli.Command{
	Name: "did-key",
	Flags: []cli.Flag{
		&cli.StringFlag{
			Name: "keypath",
		},
	},
	Action: func(cctx *cli.Context) error {
		sigkey, err := cliutil.LoadKeyFromFile(cctx.String("keypath"))
		if err != nil {
			return err
		}
		fmt.Println(sigkey.Public().DID())
		return nil
	},
}
