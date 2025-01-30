package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	comatproto "github.com/bluesky-social/indigo/api/atproto"
	"github.com/bluesky-social/indigo/atproto/data"
	"github.com/bluesky-social/indigo/atproto/syntax"
	"github.com/bluesky-social/indigo/mst"
	"github.com/bluesky-social/indigo/repo"
	"github.com/bluesky-social/indigo/util"
	"github.com/bluesky-social/indigo/xrpc"

	"github.com/ipfs/go-cid"
	cbor "github.com/ipfs/go-ipld-cbor"
	ipld "github.com/ipfs/go-ipld-format"
	"github.com/urfave/cli/v2"
	"github.com/xlab/treeprint"
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
		Host: ident.PDSEndpoint(),
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
	r, err := repo.ReadRepoFromCar(ctx, fi)
	if err != nil {
		return fmt.Errorf("failed to parse repo CAR file: %w", err)
	}

	err = r.ForEach(ctx, "", func(k string, v cid.Cid) error {
		fmt.Printf("%s\t%s\n", k, v.String())
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
	// read repository tree in to memory
	r, err := repo.ReadRepoFromCar(ctx, inputCAR)
	if err != nil {
		return err
	}
	cst := util.CborStore(r.Blockstore())
	// determine which root cid to use, defaulting to repo data root
	rootCID := r.DataCid()
	if opts.root != "" {
		optsRootCID, err := cid.Decode(opts.root)
		if err != nil {
			return err
		}
		rootCID = optsRootCID
	}
	// start walking mst
	exists, err := nodeExists(ctx, cst, rootCID)
	if err != nil {
		return err
	}
	tree := treeprint.NewWithRoot(displayCID(&rootCID, exists, opts))
	if exists {
		if err := walkMST(ctx, cst, rootCID, tree, opts); err != nil {
			return err
		}
	}
	// print tree
	fmt.Println(tree.String())
	return nil
}

func walkMST(ctx context.Context, cst *cbor.BasicIpldStore, cid cid.Cid, tree treeprint.Tree, opts repoMSTOptions) error {
	var node mst.NodeData
	if err := cst.Get(ctx, cid, &node); err != nil {
		return err
	}
	if node.Left != nil {
		exists, err := nodeExists(ctx, cst, *node.Left)
		if err != nil {
			return err
		}
		subtree := tree.AddBranch(displayCID(node.Left, exists, opts))
		if exists {
			if err := walkMST(ctx, cst, *node.Left, subtree, opts); err != nil {
				return err
			}
		}
	}
	for _, entry := range node.Entries {
		exists, err := nodeExists(ctx, cst, entry.Val)
		if err != nil {
			return err
		}
		tree.AddNode(displayEntryVal(&entry, exists, opts))
		if entry.Tree != nil {
			exists, err := nodeExists(ctx, cst, *entry.Tree)
			if err != nil {
				return err
			}
			subtree := tree.AddBranch(displayCID(entry.Tree, exists, opts))
			if exists {
				if err := walkMST(ctx, cst, *entry.Tree, subtree, opts); err != nil {
					return err
				}
			}
		}
	}
	return nil
}

func displayEntryVal(entry *mst.TreeEntry, exists bool, opts repoMSTOptions) string {
	key := string(entry.KeySuffix)
	divider := " "
	if opts.fullCID {
		divider = "\n"
	}
	return strings.Repeat("∙", int(entry.PrefixLen)) + key + divider + displayCID(&entry.Val, exists, opts)
}

func displayCID(cid *cid.Cid, exists bool, opts repoMSTOptions) string {
	cidDisplay := cid.String()
	if !opts.fullCID {
		cidDisplay = "…" + string(cidDisplay[len(cidDisplay)-7:])
	}
	connector := "─◉"
	if !exists {
		connector = "─◌"
	}
	return "[" + cidDisplay + "]" + connector
}

type repoMSTOptions struct {
	carPath string
	fullCID bool
	root    string
}

func nodeExists(ctx context.Context, cst *cbor.BasicIpldStore, cid cid.Cid) (bool, error) {
	if _, err := cst.Blocks.Get(ctx, cid); err != nil {
		if errors.Is(err, ipld.ErrNotFound{}) {
			return false, nil
		}
		return false, err
	}
	return true, nil
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
