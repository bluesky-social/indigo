package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"

	"github.com/bluesky-social/indigo/api/agnostic"
	comatproto "github.com/bluesky-social/indigo/api/atproto"
	"github.com/bluesky-social/indigo/atproto/data"
	"github.com/bluesky-social/indigo/atproto/identity"
	"github.com/bluesky-social/indigo/atproto/syntax"
	"github.com/bluesky-social/indigo/xrpc"

	"github.com/urfave/cli/v2"
)

var cmdRecord = &cli.Command{
	Name:  "record",
	Usage: "sub-commands for repo records",
	Flags: []cli.Flag{},
	Subcommands: []*cli.Command{
		cmdRecordGet,
		cmdRecordList,
		&cli.Command{
			Name:      "create",
			Usage:     "create record from JSON",
			ArgsUsage: `<file>`,
			Flags: []cli.Flag{
				&cli.StringFlag{
					Name:    "rkey",
					Aliases: []string{"r"},
					Usage:   "record key",
				},
				&cli.BoolFlag{
					Name:    "no-validate",
					Aliases: []string{"n"},
					Usage:   "tells PDS not to validate record Lexicon schema",
				},
			},
			Action: runRecordCreate,
		},
		&cli.Command{
			Name:      "update",
			Usage:     "replace existing record from JSON",
			ArgsUsage: `<file>`,
			Flags: []cli.Flag{
				&cli.StringFlag{
					Name:     "rkey",
					Aliases:  []string{"r"},
					Required: true,
					Usage:    "record key",
				},
				&cli.BoolFlag{
					Name:    "no-validate",
					Aliases: []string{"n"},
					Usage:   "tells PDS not to validate record Lexicon schema",
				},
			},
			Action: runRecordUpdate,
		},
		&cli.Command{
			Name:  "delete",
			Usage: "delete an existing record",
			Flags: []cli.Flag{
				&cli.StringFlag{
					Name:     "collection",
					Aliases:  []string{"c"},
					Required: true,
					Usage:    "collection (NSID)",
				},
				&cli.StringFlag{
					Name:     "rkey",
					Aliases:  []string{"r"},
					Required: true,
					Usage:    "record key",
				},
			},
			Action: runRecordDelete,
		},
	},
}

var cmdRecordGet = &cli.Command{
	Name:      "get",
	Usage:     "fetch record from the network",
	ArgsUsage: `<at-uri>`,
	Flags:     []cli.Flag{},
	Action:    runRecordGet,
}

var cmdRecordList = &cli.Command{
	Name:      "ls",
	Aliases:   []string{"list"},
	Usage:     "list all records for an account",
	ArgsUsage: `<at-identifier>`,
	Flags: []cli.Flag{
		&cli.StringFlag{
			Name:  "collection",
			Usage: "only list records from a specific collection",
		},
		&cli.BoolFlag{
			Name:    "collections",
			Aliases: []string{"c"},
			Usage:   "list collections, not individual record paths",
		},
	},
	Action: runRecordList,
}

func runRecordGet(cctx *cli.Context) error {
	ctx := context.Background()
	dir := identity.DefaultDirectory()

	uriArg := cctx.Args().First()
	if uriArg == "" {
		return fmt.Errorf("expected a single AT-URI argument")
	}

	aturi, err := syntax.ParseATURI(uriArg)
	if err != nil {
		return fmt.Errorf("not a valid AT-URI: %v", err)
	}
	ident, err := dir.Lookup(ctx, aturi.Authority())
	if err != nil {
		return err
	}

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

func runRecordList(cctx *cli.Context) error {
	ctx := context.Background()
	username := cctx.Args().First()
	if username == "" {
		return fmt.Errorf("need to provide username as an argument")
	}
	ident, err := resolveIdent(ctx, username)
	if err != nil {
		return err
	}

	// create a new API client to connect to the account's PDS
	xrpcc := xrpc.Client{
		Host: ident.PDSEndpoint(),
	}
	if xrpcc.Host == "" {
		return fmt.Errorf("no PDS endpoint for identity")
	}

	desc, err := comatproto.RepoDescribeRepo(ctx, &xrpcc, ident.DID.String())
	if err != nil {
		return err
	}
	if cctx.Bool("collections") {
		for _, nsid := range desc.Collections {
			fmt.Printf("%s\n", nsid)
		}
		return nil
	}
	collections := desc.Collections
	filter := cctx.String("collection")
	if filter != "" {
		collections = []string{filter}
	}

	for _, nsid := range collections {
		cursor := ""
		for {
			// collection string, cursor string, limit int64, repo string, reverse bool, rkeyEnd string, rkeyStart string
			resp, err := agnostic.RepoListRecords(ctx, &xrpcc, nsid, cursor, 100, ident.DID.String(), false, "", "")
			if err != nil {
				return err
			}
			for _, rec := range resp.Records {
				aturi, err := syntax.ParseATURI(rec.Uri)
				if err != nil {
					return err
				}
				fmt.Printf("%s\t%s\t%s\n", aturi.Collection(), aturi.RecordKey(), rec.Cid)
			}
			if resp.Cursor != nil && *resp.Cursor != "" {
				cursor = *resp.Cursor
			} else {
				break
			}
		}
	}

	return nil
}

func runRecordCreate(cctx *cli.Context) error {
	ctx := context.Background()
	recordPath := cctx.Args().First()
	if recordPath == "" {
		return fmt.Errorf("need to provide file path as an argument")
	}

	xrpcc, err := loadAuthClient(ctx)
	if err == ErrNoAuthSession {
		return fmt.Errorf("auth required, but not logged in")
	} else if err != nil {
		return err
	}

	recordBytes, err := os.ReadFile(recordPath)
	if err != nil {
		return err
	}

	recordVal, err := data.UnmarshalJSON(recordBytes)
	if err != nil {
		return err
	}

	nsid, err := data.ExtractTypeJSON(recordBytes)
	if err != nil {
		return err
	}

	var rkey *string
	if cctx.String("rkey") != "" {
		rk, err := syntax.ParseRecordKey(cctx.String("rkey"))
		if err != nil {
			return err
		}
		s := rk.String()
		rkey = &s
	}
	validate := !cctx.Bool("no-validate")

	resp, err := agnostic.RepoCreateRecord(ctx, xrpcc, &agnostic.RepoCreateRecord_Input{
		Collection: nsid,
		Repo:       xrpcc.Auth.Did,
		Record:     recordVal,
		Rkey:       rkey,
		Validate:   &validate,
	})
	if err != nil {
		return err
	}

	fmt.Printf("%s\t%s\n", resp.Uri, resp.Cid)
	return nil
}

func runRecordUpdate(cctx *cli.Context) error {
	ctx := context.Background()
	recordPath := cctx.Args().First()
	if recordPath == "" {
		return fmt.Errorf("need to provide file path as an argument")
	}

	xrpcc, err := loadAuthClient(ctx)
	if err == ErrNoAuthSession {
		return fmt.Errorf("auth required, but not logged in")
	} else if err != nil {
		return err
	}

	recordBytes, err := os.ReadFile(recordPath)
	if err != nil {
		return err
	}

	recordVal, err := data.UnmarshalJSON(recordBytes)
	if err != nil {
		return err
	}

	nsid, err := data.ExtractTypeJSON(recordBytes)
	if err != nil {
		return err
	}

	rkey := cctx.String("rkey")

	// NOTE: need to fetch existing record CID to perform swap. this is optional in theory, but golang can't deal with "optional" and "nullable", so we always need to set this (?)
	existing, err := agnostic.RepoGetRecord(ctx, xrpcc, "", nsid, xrpcc.Auth.Did, rkey)
	if err != nil {
		return err
	}

	validate := !cctx.Bool("no-validate")

	resp, err := agnostic.RepoPutRecord(ctx, xrpcc, &agnostic.RepoPutRecord_Input{
		Collection: nsid,
		Repo:       xrpcc.Auth.Did,
		Record:     recordVal,
		Rkey:       rkey,
		Validate:   &validate,
		SwapRecord: existing.Cid,
	})
	if err != nil {
		return err
	}

	fmt.Printf("%s\t%s\n", resp.Uri, resp.Cid)
	return nil
}

func runRecordDelete(cctx *cli.Context) error {
	ctx := context.Background()

	xrpcc, err := loadAuthClient(ctx)
	if err == ErrNoAuthSession {
		return fmt.Errorf("auth required, but not logged in")
	} else if err != nil {
		return err
	}

	rkey, err := syntax.ParseRecordKey(cctx.String("rkey"))
	if err != nil {
		return err
	}
	collection, err := syntax.ParseNSID(cctx.String("collection"))
	if err != nil {
		return err
	}

	_, err = comatproto.RepoDeleteRecord(ctx, xrpcc, &comatproto.RepoDeleteRecord_Input{
		Collection: collection.String(),
		Repo:       xrpcc.Auth.Did,
		Rkey:       rkey.String(),
	})
	if err != nil {
		return err
	}
	return nil
}
