package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	comatproto "github.com/bluesky-social/indigo/api/atproto"
	"github.com/bluesky-social/indigo/atproto/data"
	"github.com/bluesky-social/indigo/atproto/repo"
	"github.com/bluesky-social/indigo/atproto/syntax"
	"github.com/bluesky-social/indigo/util"
	"github.com/bluesky-social/indigo/xrpc"

	"github.com/ipfs/go-cid"
	"github.com/urfave/cli/v2"
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
			Name:      "mst",
			Usage:     "show repo MST structure",
			ArgsUsage: `<car-file>`,
			Flags: []cli.Flag{
				&cli.BoolFlag{
					Name:    "full-cid",
					Aliases: []string{"f"},
					Usage:   "display full CIDs",
				},
				&cli.StringFlag{
					Name:    "root",
					Aliases: []string{"r"},
					Usage:   "CID of root block",
				},
			},
			Action: runRepoMST,
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
		Host:      ident.PDSEndpoint(),
		UserAgent: userAgent(),
	}
	if xrpcc.Host == "" {
		return fmt.Errorf("no PDS endpoint for identity")
	}

	// set longer timeout, for large CAR files
	xrpcc.Client = util.RobustHTTPClient()
	xrpcc.Client.Timeout = 600 * time.Second

	carPath := cctx.String("output")
	if carPath == "" {
		// NOTE: having the rev in the the path might be nice
		now := time.Now().Format("20060102150405")
		carPath = fmt.Sprintf("%s.%s.car", username, now)
	}
	output, err := getFileOrStdout(carPath)
	if err != nil {
		if errors.Is(err, os.ErrExist) {
			return fmt.Errorf("file already exists: %s", carPath)
		}
		return err
	}
	defer output.Close()
	if carPath != stdIOPath {
		fmt.Printf("downloading from %s to: %s\n", xrpcc.Host, carPath)
	}
	repoBytes, err := comatproto.SyncGetRepo(ctx, &xrpcc, ident.DID.String(), "")
	if err != nil {
		return err
	}
	if _, err := output.Write(repoBytes); err != nil {
		return err
	}
	return nil
}

func runRepoImport(cctx *cli.Context) error {
	ctx := context.Background()

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

	err = comatproto.RepoImportRepo(ctx, xrpcc, bytes.NewReader(fileBytes))
	if err != nil {
		return fmt.Errorf("failed to import repo: %w", err)
	}

	return nil
}

func runRepoList(cctx *cli.Context) error {
	ctx := context.Background()
	carPath := cctx.Args().First()
	if carPath == "" {
		return fmt.Errorf("need to provide path to CAR file as argument")
	}
	fi, err := os.Open(carPath)
	if err != nil {
		return fmt.Errorf("failed to open CAR file: %w", err)
	}

	// read repository tree in to memory
	_, r, err := repo.LoadRepoFromCAR(ctx, fi)
	if err != nil {
		return fmt.Errorf("failed to parse repo CAR file: %w", err)
	}

	err = r.MST.Walk(func(k []byte, v cid.Cid) error {
		fmt.Printf("%s\t%s\n", string(k), v.String())
		return nil
	})
	if err != nil {
		return fmt.Errorf("failed to read records from repo CAR file: %w", err)
	}
	return nil
}

func runRepoInspect(cctx *cli.Context) error {
	ctx := context.Background()
	carPath := cctx.Args().First()
	if carPath == "" {
		return fmt.Errorf("need to provide path to CAR file as argument")
	}
	fi, err := os.Open(carPath)
	if err != nil {
		return err
	}

	// read repository tree in to memory
	c, _, err := repo.LoadRepoFromCAR(ctx, fi)
	if err != nil {
		return err
	}

	fmt.Printf("ATProto Repo Spec Version: %d\n", c.Version)
	fmt.Printf("DID: %s\n", c.DID)
	fmt.Printf("Data CID: %s\n", c.Data)
	fmt.Printf("Prev CID: %s\n", c.Prev)
	fmt.Printf("Revision: %s\n", c.Rev)
	// TODO: Signature?

	return nil
}

func runRepoMST(cctx *cli.Context) error {
	ctx := context.Background()
	opts := repoMSTOptions{
		carPath: cctx.Args().First(),
		fullCID: cctx.Bool("full-cid"),
		root:    cctx.String("root"),
	}
	// read from file or stdin
	if opts.carPath == "" {
		return fmt.Errorf("need to provide path to CAR file as argument")
	}
	inputCAR, err := getFileOrStdin(opts.carPath)
	if err != nil {
		return err
	}
	return prettyMST(ctx, inputCAR, opts)
}

func runRepoUnpack(cctx *cli.Context) error {
	ctx := context.Background()
	carPath := cctx.Args().First()
	if carPath == "" {
		return fmt.Errorf("need to provide path to CAR file as argument")
	}
	fi, err := os.Open(carPath)
	if err != nil {
		return err
	}

	c, r, err := repo.LoadRepoFromCAR(ctx, fi)
	if err != nil {
		return err
	}

	// extract DID from repo commit
	did, err := syntax.ParseDID(c.DID)
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
	commitJSON, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return err
	}
	if err := os.WriteFile(commitPath, commitJSON, 0666); err != nil {
		return err
	}

	// then all the actual records
	err = r.MST.Walk(func(k []byte, v cid.Cid) error {
		col, rkey, err := syntax.ParseRepoPath(string(k))
		if err != nil {
			return err
		}
		recBytes, _, err := r.GetRecordBytes(ctx, col, rkey)
		if err != nil {
			return err
		}

		rec, err := data.UnmarshalCBOR(recBytes)
		if err != nil {
			return err
		}

		recPath := topDir + "/" + string(k)
		fmt.Printf("%s.json\n", recPath)
		err = os.MkdirAll(filepath.Dir(recPath), os.ModePerm)
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
