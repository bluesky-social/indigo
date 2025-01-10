package main

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/bluesky-social/indigo/api/agnostic"
	"github.com/bluesky-social/indigo/atproto/data"
	"github.com/bluesky-social/indigo/atproto/identity"
	"github.com/bluesky-social/indigo/atproto/lexicon"
	"github.com/bluesky-social/indigo/atproto/syntax"
	"github.com/bluesky-social/indigo/xrpc"

	"github.com/urfave/cli/v2"
)

var cmdLex = &cli.Command{
	Name:  "lex",
	Usage: "sub-commands for Lexicons",
	Flags: []cli.Flag{},
	Subcommands: []*cli.Command{
		&cli.Command{
			Name:      "resolve",
			Usage:     "lookup a schema for an NSID",
			ArgsUsage: `<nsid>`,
			Flags: []cli.Flag{
				&cli.BoolFlag{
					Name:  "did",
					Usage: "just resolve to DID, not the schema itself",
				},
			},
			Action: runLexResolve,
		},
		&cli.Command{
			Name:      "parse",
			Usage:     "parse and validate Lexicon schema files",
			ArgsUsage: `<path>+`,
			Flags:     []cli.Flag{},
			Action:    runLexParse,
		},
		&cli.Command{
			Name:      "publish",
			Usage:     "add schema JSON files to atproto repo",
			ArgsUsage: `<path>+`,
			Flags:     []cli.Flag{},
			Action:    runLexPublish,
		},
		&cli.Command{
			Name:      "ls",
			Aliases:   []string{"list"},
			Usage:     "list all known Lexicon NSIDs at the same level of hierarchy",
			ArgsUsage: `<nsid>`,
			Flags:     []cli.Flag{},
			Action:    runLexList,
		},
		&cli.Command{
			Name:      "validate",
			Usage:     "validate a record, either AT-URI or local file",
			ArgsUsage: `<uri-or-path>`,
			Flags: []cli.Flag{
				&cli.BoolFlag{
					Name:  "allow-legacy-blob",
					Usage: "be permissive of legacy blobs",
				},
				&cli.StringFlag{
					Name:    "catalog",
					Aliases: []string{"c"},
					Usage:   "path to directory of Lexicon files",
				},
			},
			Action: runLexValidate,
		},
	},
}

func loadSchemaFile(p string) (map[string]any, error) {
	f, err := os.Open(p)
	if err != nil {
		return nil, err
	}
	defer func() { _ = f.Close() }()
	b, err := io.ReadAll(f)
	if err != nil {
		return nil, err
	}

	// verify format
	var sf lexicon.SchemaFile
	if err := json.Unmarshal(b, &sf); err != nil {
		return nil, err
	}
	// TODO: additional validation?

	// parse as raw data
	d, err := data.UnmarshalJSON(b)
	if err != nil {
		return nil, err
	}
	return d, nil
}

func runLexParse(cctx *cli.Context) error {
	if cctx.Args().Len() <= 0 {
		return fmt.Errorf("require at least one path to parse")
	}
	for _, path := range cctx.Args().Slice() {
		_, err := loadSchemaFile(path)
		if err != nil {
			return fmt.Errorf("failed to parse %s: %w", path, err)
		}
		fmt.Printf("%s: success\n", path)
	}
	return nil
}

func runLexPublish(cctx *cli.Context) error {
	if cctx.Args().Len() <= 0 {
		return fmt.Errorf("require at least one path to publish")
	}

	ctx := cctx.Context
	xrpcc, err := loadAuthClient(ctx)
	if err == ErrNoAuthSession {
		return fmt.Errorf("auth required, but not logged in")
	} else if err != nil {
		return err
	}

	validateFlag := false

	for _, path := range cctx.Args().Slice() {
		recordVal, err := loadSchemaFile(path)
		if err != nil {
			return fmt.Errorf("failed to parse %s: %w", path, err)
		}

		recordVal["$type"] = "com.atproto.lexicon.schema"
		val, ok := recordVal["id"]
		if !ok {
			return fmt.Errorf("missing NSID in Lexicon schema")
		}
		rawNSID, ok := val.(string)
		if !ok {
			return fmt.Errorf("missing NSID in Lexicon schema")
		}
		nsid, err := syntax.ParseNSID(rawNSID)
		if err != nil {
			return err
		}
		nsidStr := nsid.String()

		resp, err := agnostic.RepoPutRecord(ctx, xrpcc, &agnostic.RepoPutRecord_Input{
			Collection: "com.atproto.lexicon.schema",
			Repo:       xrpcc.Auth.Did,
			Record:     recordVal,
			Rkey:       nsidStr,
			Validate:   &validateFlag,
		})
		if err != nil {
			return err
		}

		fmt.Printf("%s\t%s\n", resp.Uri, resp.Cid)
	}
	return nil
}

func runLexResolve(cctx *cli.Context) error {
	ctx := cctx.Context
	raw := cctx.Args().First()
	if raw == "" {
		return fmt.Errorf("NSID argument is required")
	}

	// TODO: handle fragments
	nsid, err := syntax.ParseNSID(raw)
	if err != nil {
		return err
	}

	dir := identity.BaseDirectory{}
	if cctx.Bool("did") {
		did, err := dir.ResolveNSID(ctx, nsid)
		if err != nil {
			return err
		}
		fmt.Println(did)
		return nil
	}

	data, err := lexicon.ResolveLexiconData(ctx, &dir, nsid)
	if err != nil {
		return err
	}

	b, err := json.MarshalIndent(data, "", "  ")
	if err != nil {
		return err
	}
	fmt.Println(string(b))

	return nil
}

func runLexList(cctx *cli.Context) error {
	ctx := cctx.Context
	raw := cctx.Args().First()
	if raw == "" {
		return fmt.Errorf("NSID argument is required")
	}

	// TODO: handle fragments?
	nsid, err := syntax.ParseNSID(raw)
	if err != nil {
		return err
	}
	authority := nsid.Authority()

	dir := identity.BaseDirectory{}
	did, err := dir.ResolveNSID(ctx, nsid)
	if err != nil {
		return err
	}

	ident, err := dir.LookupDID(ctx, did)
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

	// iterate through all records in the lexicon schema collection, and check if prefix ("authority") matches that of the original NSID
	// NOTE: much of this code is copied from runRecordList
	cursor := ""
	for {
		// collection string, cursor string, limit int64, repo string, reverse bool, rkeyEnd string, rkeyStart string
		resp, err := agnostic.RepoListRecords(ctx, &xrpcc, "com.atproto.lexicon.schema", cursor, 100, ident.DID.String(), false, "", "")
		if err != nil {
			return err
		}
		for _, rec := range resp.Records {
			aturi, err := syntax.ParseATURI(rec.Uri)
			if err != nil {
				return err
			}
			schemaNSID, err := syntax.ParseNSID(aturi.RecordKey().String())
			if err != nil {
				continue
			}
			if schemaNSID.Authority() == authority {
				fmt.Println(schemaNSID)
			}
		}
		if resp.Cursor != nil && *resp.Cursor != "" {
			cursor = *resp.Cursor
		} else {
			break
		}
	}

	return nil
}

func runLexValidate(cctx *cli.Context) error {
	ctx := cctx.Context
	ref := cctx.Args().First()
	if ref == "" {
		return fmt.Errorf("URI or file path argument is required")
	}

	var nsid syntax.NSID
	var recordData map[string]any
	dir := identity.BaseDirectory{}
	cat := lexicon.NewResolvingCatalog()

	var flags lexicon.ValidateFlags = 0
	if cctx.Bool("allow-legacy-blob") {
		flags |= lexicon.AllowLegacyBlob
	}

	if cctx.String("catalog") != "" {
		fmt.Printf("loading catalog directory: %s\n", cctx.String("catalog"))
		if err := cat.Base.LoadDirectory(cctx.String("catalog")); err != nil {
			return err
		}
	}

	// fetch from network if an AT-URI
	if strings.HasPrefix(ref, "at://") {
		aturi, err := syntax.ParseATURI(ref)
		if err != nil {
			return err
		}
		nsid = aturi.Collection()

		ident, err := dir.Lookup(ctx, aturi.Authority())
		if err != nil {
			return err
		}

		recordData, err = fetchRecord(ctx, *ident, aturi)
		if err != nil {
			return err
		}
	} else {
		// otherwise try to read from disk
		recordBytes, err := os.ReadFile(ref)
		if err != nil {
			return err
		}

		rawNSID, err := data.ExtractTypeJSON(recordBytes)
		if err != nil {
			return err
		}
		nsid, err = syntax.ParseNSID(rawNSID)
		if err != nil {
			return err
		}

		recordData, err = data.UnmarshalJSON(recordBytes)
		if err != nil {
			return err
		}
	}

	if err := lexicon.ValidateRecord(&cat, recordData, nsid.String(), flags); err != nil {
		return err
	}
	fmt.Printf("valid %s record\n", nsid)
	return nil
}
