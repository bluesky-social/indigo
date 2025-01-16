package main

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/bluesky-social/indigo/atproto/identity"
	"github.com/bluesky-social/indigo/atproto/syntax"

	"github.com/urfave/cli/v2"
)

var cmdResolve = &cli.Command{
	Name:      "resolve",
	Usage:     "lookup identity metadata",
	ArgsUsage: `<at-identifier>`,
	Flags: []cli.Flag{
		&cli.BoolFlag{
			Name:  "did",
			Usage: "just resolve to DID",
		},
	},
	Action: runResolve,
}

func runResolve(cctx *cli.Context) error {
	ctx := context.Background()
	s := cctx.Args().First()
	if s == "" {
		return fmt.Errorf("need to provide account identifier as an argument")
	}

	atid, err := syntax.ParseAtIdentifier(s)
	if err != nil {
		return err
	}
	dir := identity.BaseDirectory{}
	var doc *identity.DIDDocument

	if cctx.Bool("did") {
		if atid.IsDID() {
		}
	}

	if atid.IsDID() {
		did, err := atid.AsDID()
		if err != nil {
			return err
		}
		if cctx.Bool("did") {
			fmt.Println(did)
			return nil
		}
		doc, err = dir.ResolveDID(ctx, did)
		if err != nil {
			return err
		}
	} else {
		handle, err := atid.AsHandle()
		if err != nil {
			return err
		}
		did, err := dir.ResolveHandle(ctx, handle)
		if err != nil {
			return err
		}
		if cctx.Bool("did") {
			fmt.Println(did)
			return nil
		}
		doc, err = dir.ResolveDID(ctx, did)
		if err != nil {
			return err
		}

		ident := identity.ParseIdentity(doc)
		decl, err := ident.DeclaredHandle()
		if err != nil {
			return err
		}
		if handle != decl {
			return fmt.Errorf("invalid handle")
		}
	}

	// TODO: actually print DID doc instead of JSON version of identity
	b, err := json.MarshalIndent(doc, "", "  ")
	if err != nil {
		return err
	}

	fmt.Println(string(b))
	return nil
}
