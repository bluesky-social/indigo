package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"

	"github.com/bluesky-social/indigo/atproto/identity"
	"github.com/bluesky-social/indigo/atproto/syntax"

	"github.com/urfave/cli/v2"
)

func main() {
	app := cli.App{
		Name:  "atp-id",
		Usage: "informal debugging CLI tool for atproto identities",
	}
	app.Commands = []*cli.Command{
		&cli.Command{
			Name:   "lookup",
			Usage:  "fully resolve an at-identifier (DID or handle)",
			Action: runLookup,
		},
		&cli.Command{
			Name:   "resolve-handle",
			Usage:  "resolve a handle to DID",
			Action: runResolveHandle,
		},
		&cli.Command{
			Name:   "resolve-did",
			Usage:  "resolve a DID to DID Document",
			Action: runResolveDID,
		},
	}
	app.RunAndExitOnError()
}

func runLookup(cctx *cli.Context) error {
	ctx := context.Background()
	s := cctx.Args().First()
	if s == "" {
		return fmt.Errorf("need to provide identifier as an argument")
	}

	id, err := syntax.ParseAtIdentifier(s)
	if err != nil {
		return err
	}
	fmt.Printf("valid at-identifier syntax: %s\n", id)

	ncat := identity.NewBasicCatalog("https://plc.directory")

	acc, err := ncat.Lookup(ctx, *id)
	if err != nil {
		return err
	}
	fmt.Println(acc)
	return nil
}

func runResolveHandle(cctx *cli.Context) error {
	ctx := context.Background()
	s := cctx.Args().First()
	if s == "" {
		return fmt.Errorf("need to provide handle as an argument")
	}

	handle, err := syntax.ParseHandle(s)
	if err != nil {
		return err
	}
	fmt.Printf("valid handle syntax: %s\n", handle)

	did, err := identity.ResolveHandle(ctx, handle)
	if err != nil {
		return err
	}
	fmt.Println(did)
	return nil
}

func runResolveDID(cctx *cli.Context) error {
	ctx := context.Background()
	s := cctx.Args().First()
	if s == "" {
		fmt.Println("need to provide DID as an argument")
		os.Exit(-1)
	}

	did, err := syntax.ParseDID(s)
	if err != nil {
		fmt.Println(err)
		os.Exit(-1)
	}
	fmt.Printf("valid DID syntax: %s\n", did)

	doc, err := identity.DefaultResolveDID(ctx, did)
	if err != nil {
		return err
	}
	jsonBytes, err := json.Marshal(&doc)
	if err != nil {
		return err
	}
	fmt.Println(string(jsonBytes))
	return nil
}
