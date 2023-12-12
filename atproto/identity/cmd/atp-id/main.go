package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
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
			Name:      "lookup",
			Usage:     "fully resolve an at-identifier (DID or handle)",
			ArgsUsage: "<at-identifier>",
			Action:    runLookup,
		},
		&cli.Command{
			Name:      "resolve-handle",
			Usage:     "resolve a handle to DID",
			ArgsUsage: "<handle>",
			Action:    runResolveHandle,
		},
		&cli.Command{
			Name:      "resolve-did",
			Usage:     "resolve a DID to DID Document",
			ArgsUsage: "<did>",
			Action:    runResolveDID,
		},
	}
	h := slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelDebug})
	slog.SetDefault(slog.New(h))
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
	slog.Info("valid syntax", "at-identifier", id)

	dir := identity.DefaultDirectory()
	acc, err := dir.Lookup(ctx, *id)
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
	slog.Info("valid syntax", "handle", handle)

	d := identity.BaseDirectory{}
	did, err := d.ResolveHandle(ctx, handle)
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
	slog.Info("valid syntax", "did", did)

	d := identity.BaseDirectory{}
	doc, err := d.ResolveDID(ctx, did)
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
