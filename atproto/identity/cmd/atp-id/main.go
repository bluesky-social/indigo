package main

import (
	"fmt"
	"os"

	//"github.com/bluesky-social/indigo/atproto/identity"
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
			Name:   "resolve-handle",
			Usage:  "resolve a handle",
			Action: runResolveHandle,
		},
		&cli.Command{
			Name:   "resolve-did",
			Usage:  "resolve a DID",
			Action: runResolveDID,
		},
	}
	app.RunAndExitOnError()
}

func runResolveHandle(cctx *cli.Context) error {
	s := cctx.Args().First()
	if s == "" {
		fmt.Println("need to provide handle as an argument")
		os.Exit(-1)
	}

	handle, err := syntax.ParseHandle(s)
	if err != nil {
		fmt.Println(err)
		os.Exit(-1)
	}
	fmt.Printf("valid handle syntax: %s\n", handle)
	return nil
}

func runResolveDID(cctx *cli.Context) error {
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
	return nil
}
