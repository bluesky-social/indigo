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
	Flags:     []cli.Flag{},
	Action:    runResolve,
}

func runResolve(cctx *cli.Context) error {
	ctx := context.Background()
	s := cctx.Args().First()
	if s == "" {
		return fmt.Errorf("need to provide account identifier as an argument")
	}

	id, err := syntax.ParseAtIdentifier(s)
	if err != nil {
		return err
	}

	dir := identity.DefaultDirectory()
	acc, err := dir.Lookup(ctx, *id)
	if err != nil {
		return err
	}

	// TODO: actually print DID doc instead of JSON version of identity
	b, err := json.MarshalIndent(acc, "", "  ")
	if err != nil {
		return err
	}

	fmt.Println(string(b))
	return nil
}
