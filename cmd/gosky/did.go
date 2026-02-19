package main

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/bluesky-social/indigo/atproto/identity"
	"github.com/bluesky-social/indigo/atproto/syntax"
	"github.com/bluesky-social/indigo/util/cliutil"

	"github.com/urfave/cli/v2"
)

var didCmd = &cli.Command{
	Name:  "did",
	Usage: "sub-commands for working with DIDs",
	Flags: []cli.Flag{},
	Subcommands: []*cli.Command{
		didGetCmd,
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
		ctx := context.TODO()
		did := cctx.Args().First()

		bdir := identity.BaseDirectory{}

		if cctx.Bool("handle") {
			id, err := bdir.LookupDID(ctx, syntax.DID(did))
			if err != nil {
				return err
			}

			fmt.Println(id.Handle)
			return nil
		}

		doc, err := bdir.ResolveDID(ctx, syntax.DID(did))
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
		pubkey, err := sigkey.PublicKey()
		if err != nil {
			return err
		}
		fmt.Println(pubkey.DIDKey())
		return nil
	},
}
