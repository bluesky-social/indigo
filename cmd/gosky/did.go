package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/bluesky-social/indigo/atproto/atcrypto"
	"github.com/bluesky-social/indigo/atproto/identity"
	"github.com/bluesky-social/indigo/atproto/syntax"
	"github.com/bluesky-social/indigo/util/cliutil"

	"github.com/did-method-plc/go-didplc"
	"github.com/urfave/cli/v2"
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
		ctx := context.Background()
		plcc := didplc.Client{}

		args, err := needArgs(cctx, "handle", "service")
		if err != nil {
			return err
		}
		handle, service := args[0], args[1]

		recoverydid := cctx.String("recoverydid")

		keybytes, err := os.ReadFile(cctx.String("signingkey"))
		if err != nil {
			return err
		}

		priv, err := atcrypto.ParsePrivateMultibase(strings.TrimSpace(string(keybytes)))
		if err != nil {
			return err
		}
		pub, err := priv.PublicKey()

		fmt.Println("KEYDID: ", pub.DIDKey())

		op := didplc.RegularOp{
			Type:         "plc_operation",
			RotationKeys: []string{recoverydid},
			VerificationMethods: map[string]string{
				"atproto": pub.DIDKey(),
			},
			AlsoKnownAs: []string{"at://" + handle},
			Services: map[string]didplc.OpService{
				"atproto_pds": didplc.OpService{
					Type:     "AtprotoPersonalDataServer",
					Endpoint: service,
				},
			},
			Prev: nil,
			Sig:  nil,
		}
		if err := op.Sign(priv); err != nil {
			return err
		}
		did, err := op.DID()
		if err != nil {
			return err
		}

		if err := plcc.Submit(ctx, did, &op); err != nil {
			return err
		}

		fmt.Println(did)
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
