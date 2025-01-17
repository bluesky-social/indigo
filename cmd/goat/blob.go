package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"

	comatproto "github.com/bluesky-social/indigo/api/atproto"
	"github.com/bluesky-social/indigo/xrpc"

	"github.com/urfave/cli/v2"
)

var cmdBlob = &cli.Command{
	Name:  "blob",
	Usage: "sub-commands for blobs",
	Flags: []cli.Flag{},
	Subcommands: []*cli.Command{
		&cli.Command{
			Name:      "export",
			Usage:     "download all blobs for given account",
			ArgsUsage: `<at-identifier>`,
			Flags: []cli.Flag{
				&cli.StringFlag{
					Name:    "output",
					Aliases: []string{"o"},
					Usage:   "directory to store blobs in",
				},
				&cli.StringFlag{
					Name:  "pds-host",
					Usage: "URL of the PDS to export blobs from (overrides DID doc)",
				},
			},
			Action: runBlobExport,
		},
		&cli.Command{
			Name:      "ls",
			Aliases:   []string{"list"},
			Usage:     "list all blobs for account",
			ArgsUsage: `<at-identifier>`,
			Flags:     []cli.Flag{},
			Action:    runBlobList,
		},
		&cli.Command{
			Name:      "download",
			Usage:     "download a single blob from an account",
			ArgsUsage: `<at-identifier> <cid>`,
			Flags: []cli.Flag{
				&cli.StringFlag{
					Name:    "output",
					Aliases: []string{"o"},
					Usage:   "file path to store blob at",
				},
			},
			Action: runBlobDownload,
		},
		&cli.Command{
			Name:      "upload",
			Usage:     "upload a file",
			ArgsUsage: `<file>`,
			Flags:     []cli.Flag{},
			Action:    runBlobUpload,
		},
	},
}

func runBlobExport(cctx *cli.Context) error {
	ctx := context.Background()
	username := cctx.Args().First()
	if username == "" {
		return fmt.Errorf("need to provide username as an argument")
	}
	ident, err := resolveIdent(ctx, username)
	if err != nil {
		return err
	}

	pdsHost := cctx.String("pds-host")
	if pdsHost == "" {
		pdsHost = ident.PDSEndpoint()
	}

	// create a new API client to connect to the account's PDS
	xrpcc := xrpc.Client{
		Host: pdsHost,
	}
	if xrpcc.Host == "" {
		return fmt.Errorf("no PDS endpoint for identity")
	}

	topDir := cctx.String("output")
	if topDir == "" {
		topDir = fmt.Sprintf("%s_blobs", username)
	}

	fmt.Printf("downloading blobs to: %s\n", topDir)
	os.MkdirAll(topDir, os.ModePerm)

	cursor := ""
	for {
		resp, err := comatproto.SyncListBlobs(ctx, &xrpcc, cursor, ident.DID.String(), 500, "")
		if err != nil {
			return err
		}
		for _, cidStr := range resp.Cids {
			blobPath := topDir + "/" + cidStr
			if _, err := os.Stat(blobPath); err == nil {
				fmt.Printf("%s\texists\n", blobPath)
				continue
			}
			blobBytes, err := comatproto.SyncGetBlob(ctx, &xrpcc, cidStr, ident.DID.String())
			if err != nil {
				return err
			}
			if err := os.WriteFile(blobPath, blobBytes, 0666); err != nil {
				return err
			}
			fmt.Printf("%s\tdownloaded\n", blobPath)
		}
		if resp.Cursor != nil && *resp.Cursor != "" {
			cursor = *resp.Cursor
		} else {
			break
		}
	}
	return nil
}

func runBlobList(cctx *cli.Context) error {
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

	cursor := ""
	for {
		resp, err := comatproto.SyncListBlobs(ctx, &xrpcc, cursor, ident.DID.String(), 500, "")
		if err != nil {
			return err
		}
		for _, cidStr := range resp.Cids {
			fmt.Println(cidStr)
		}
		if resp.Cursor != nil && *resp.Cursor != "" {
			cursor = *resp.Cursor
		} else {
			break
		}
	}
	return nil
}

func runBlobDownload(cctx *cli.Context) error {
	ctx := context.Background()
	username := cctx.Args().First()
	if username == "" {
		return fmt.Errorf("need to provide username as an argument")
	}
	if cctx.Args().Len() < 2 {
		return fmt.Errorf("need to provide blob CID as second argument")
	}
	blobCID := cctx.Args().Get(1)
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

	blobPath := cctx.String("output")
	if blobPath == "" {
		blobPath = blobCID
	}

	fmt.Printf("downloading blob to: %s\n", blobCID)

	if _, err := os.Stat(blobPath); err == nil {
		return fmt.Errorf("file exists: %s", blobPath)
	}
	blobBytes, err := comatproto.SyncGetBlob(ctx, &xrpcc, blobCID, ident.DID.String())
	if err != nil {
		return err
	}
	return os.WriteFile(blobPath, blobBytes, 0666)
}

func runBlobUpload(cctx *cli.Context) error {
	ctx := context.Background()
	blobPath := cctx.Args().First()
	if blobPath == "" {
		return fmt.Errorf("need to provide file path as an argument")
	}

	xrpcc, err := loadAuthClient(ctx)
	if err == ErrNoAuthSession {
		return fmt.Errorf("auth required, but not logged in")
	} else if err != nil {
		return err
	}

	fileBytes, err := os.ReadFile(blobPath)
	if err != nil {
		return err
	}

	resp, err := comatproto.RepoUploadBlob(ctx, xrpcc, bytes.NewReader(fileBytes))
	if err != nil {
		return err
	}

	b, err := json.MarshalIndent(resp.Blob, "", "  ")
	if err != nil {
		return err
	}

	fmt.Println(string(b))
	return nil
}
