package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/ipfs/go-cid"
	"github.com/urfave/cli/v2"

	"github.com/bluesky-social/indigo/api/atproto"
	"github.com/bluesky-social/indigo/atproto/data"
	"github.com/bluesky-social/indigo/atproto/syntax"
	"github.com/bluesky-social/indigo/repo"
	"github.com/bluesky-social/indigo/xrpc"
)

var cmdRepo = &cli.Command{
	Name:  "repo",
	Usage: "sub-commands for repositories",
	Flags: []cli.Flag{},
	Subcommands: []*cli.Command{
		&cli.Command{
			Name:      "export",
			Usage:     "download CAR file for given account",
			ArgsUsage: `<at-identifier>`,
			Flags: []cli.Flag{
				&cli.StringFlag{
					Name:    "output",
					Aliases: []string{"o"},
					Usage:   "file path for CAR download",
				},
			},
			Action: runRepoExport,
		},
		&cli.Command{
			Name:      "import",
			Usage:     "upload CAR file for current account",
			ArgsUsage: `<path>`,
			Action:    runRepoImport,
		},
		&cli.Command{
			Name:      "ls",
			Aliases:   []string{"list"},
			Usage:     "list records in CAR file",
			ArgsUsage: `<car-file>`,
			Flags:     []cli.Flag{},
			Action:    runRepoList,
		},
		&cli.Command{
			Name:      "inspect",
			Usage:     "show commit metadata from CAR file",
			ArgsUsage: `<car-file>`,
			Flags:     []cli.Flag{},
			Action:    runRepoInspect,
		},
		&cli.Command{
			Name:      "unpack",
			Usage:     "extract records from CAR file as directory of JSON files",
			ArgsUsage: `<car-file>`,
			Flags: []cli.Flag{
				&cli.StringFlag{
					Name:    "output",
					Aliases: []string{"o"},
					Usage:   "directory path for unpack",
				},
			},
			Action: runRepoUnpack,
		},
	},
}

func runRepoExport(cctx *cli.Context) error {
	ctx := cctx.Context
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

	carPath := cctx.String("output")
	if carPath == "" {
		// NOTE: having the rev in the the path might be nice
		now := time.Now().Format("20060102150405")
		carPath = fmt.Sprintf("%s.%s.car", username, now)
	}
	// NOTE: there is a race condition, but nice to give a friendly error earlier before downloading
	if _, err := os.Stat(carPath); err == nil {
		return fmt.Errorf("file already exists: %s", carPath)
	}
	fmt.Printf("downloading from %s to: %s\n", xrpcc.Host, carPath)
	repoBytes, err := atproto.SyncGetRepo(ctx, &xrpcc, ident.DID.String(), "")
	if err != nil {
		return err
	}
	return os.WriteFile(carPath, repoBytes, 0666)
}

func runRepoImport(cctx *cli.Context) error {
	ctx := cctx.Context

	carPath := cctx.Args().First()
	if carPath == "" {
		return fmt.Errorf("need to provide CAR file path as an argument")
	}

	xrpcc, err := loadAuthClient(ctx)
	if err == ErrNoAuthSession {
		return fmt.Errorf("auth required, but not logged in")
	} else if err != nil {
		return err
	}

	fileBytes, err := os.ReadFile(carPath)
	if err != nil {
		return err
	}

	err = atproto.RepoImportRepo(ctx, xrpcc, bytes.NewReader(fileBytes))
	if err != nil {
		return fmt.Errorf("failed to import repo: %w", err)
	}

	return nil
}

func runRepoList(cctx *cli.Context) error {
	ctx := cctx.Context
	carPath := cctx.Args().First()
	if carPath == "" {
		return fmt.Errorf("need to provide path to CAR file as argument")
	}
	fi, err := os.Open(carPath)
	if err != nil {
		return err
	}

	// read repository tree in to memory
	r, err := repo.ReadRepoFromCar(ctx, fi)
	if err != nil {
		return err
	}

	err = r.ForEach(ctx, "", func(k string, v cid.Cid) error {
		fmt.Printf("%s\t%s\n", k, v.String())
		return nil
	})
	if err != nil {
		return err
	}
	return nil
}

func runRepoInspect(cctx *cli.Context) error {
	ctx := cctx.Context
	carPath := cctx.Args().First()
	if carPath == "" {
		return fmt.Errorf("need to provide path to CAR file as argument")
	}
	fi, err := os.Open(carPath)
	if err != nil {
		return err
	}

	// read repository tree in to memory
	r, err := repo.ReadRepoFromCar(ctx, fi)
	if err != nil {
		return err
	}

	sc := r.SignedCommit()
	fmt.Printf("ATProto Repo Spec Version: %d\n", sc.Version)
	fmt.Printf("DID: %s\n", sc.Did)
	fmt.Printf("Data CID: %s\n", sc.Data)
	fmt.Printf("Prev CID: %s\n", sc.Prev)
	fmt.Printf("Revision: %s\n", sc.Rev)
	// TODO: Signature?

	return nil
}

func runRepoUnpack(cctx *cli.Context) error {
	ctx := cctx.Context
	carPath := cctx.Args().First()
	if carPath == "" {
		return fmt.Errorf("need to provide path to CAR file as argument")
	}
	fi, err := os.Open(carPath)
	if err != nil {
		return err
	}

	r, err := repo.ReadRepoFromCar(ctx, fi)
	if err != nil {
		return err
	}

	// extract DID from repo commit
	sc := r.SignedCommit()
	did, err := syntax.ParseDID(sc.Did)
	if err != nil {
		return err
	}

	topDir := cctx.String("output")
	if topDir == "" {
		topDir = did.String()
	}
	fmt.Printf("writing output to: %s\n", topDir)

	// first the commit object as a meta file
	commitPath := topDir + "/_commit.json"
	os.MkdirAll(filepath.Dir(commitPath), os.ModePerm)
	commitJSON, err := json.MarshalIndent(sc, "", "  ")
	if err != nil {
		return err
	}
	if err := os.WriteFile(commitPath, commitJSON, 0666); err != nil {
		return err
	}

	// then all the actual records
	err = r.ForEach(ctx, "", func(k string, v cid.Cid) error {
		_, recBytes, err := r.GetRecordBytes(ctx, k)
		if err != nil {
			return err
		}

		rec, err := data.UnmarshalCBOR(*recBytes)
		if err != nil {
			return err
		}

		recPath := topDir + "/" + k
		fmt.Printf("%s.json\n", recPath)
		os.MkdirAll(filepath.Dir(recPath), os.ModePerm)
		if err != nil {
			return err
		}
		recJSON, err := json.MarshalIndent(rec, "", "  ")
		if err != nil {
			return err
		}
		if err := os.WriteFile(recPath+".json", recJSON, 0666); err != nil {
			return err
		}

		return nil
	})
	if err != nil {
		return err
	}
	return nil
}
