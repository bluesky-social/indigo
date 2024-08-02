package main

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/bluesky-social/indigo/atproto/identity"
	"github.com/bluesky-social/indigo/atproto/syntax"

	"github.com/urfave/cli/v2"
)

var cmdGet = &cli.Command{
	Name:      "get",
	Usage:     "fetch record from the network",
	ArgsUsage: `<at-uri>`,
	Flags:     []cli.Flag{},
	Action:    runGet,
}

func runGet(cctx *cli.Context) error {
	ctx := context.Background()
	dir := identity.DefaultDirectory()

	uriArg := cctx.Args().First()
	if uriArg == "" {
		return fmt.Errorf("expected a single AT-URI argument")
	}

	// TODO: also handle https://bsky.app URLs, like gosky does
	aturi, err := syntax.ParseATURI(uriArg)
	if err != nil {
		return fmt.Errorf("not a valid AT-URI: %v", err)
	}
	ident, err := dir.Lookup(ctx, aturi.Authority())
	if err != nil {
		return err
	}

	// TODO: should not marshal/unmarshal against known lexicons

	record, err := fetchRecord(ctx, *ident, aturi)
	if err != nil {
		return err
	}

	b, err := json.MarshalIndent(record, "", "  ")
	if err != nil {
		return err
	}

	fmt.Println(string(b))
	return nil
}
