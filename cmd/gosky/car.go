package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/bluesky-social/indigo/atproto/syntax"
	"github.com/bluesky-social/indigo/repo"

	"github.com/ipfs/go-cid"
	cli "github.com/urfave/cli/v2"
)

var carCmd = &cli.Command{
	Name:  "car",
	Usage: "sub-commands to work with CAR files on local disk",
	Subcommands: []*cli.Command{
		carUnpackCmd,
	},
}

var carUnpackCmd = &cli.Command{
	Name:  "unpack",
	Usage: "read all records from repo export CAR file, write as JSON files in directories",
	Flags: []cli.Flag{
		&cli.BoolFlag{
			Name:  "cbor",
			Usage: "output CBOR files instead of JSON",
		},
		&cli.StringFlag{
			Name:  "out-dir",
			Usage: "directory to write files to",
		},
	},
	ArgsUsage: `<car-file>`,
	Action: func(cctx *cli.Context) error {
		ctx := context.Background()
		arg := cctx.Args().First()
		if arg == "" {
			return fmt.Errorf("CAR file path arg is required")
		}

		fi, err := os.Open(arg)
		if err != nil {
			return err
		}

		r, err := repo.ReadRepoFromCar(ctx, fi)
		if err != nil {
			return err
		}

		sc := r.SignedCommit()
		did, err := syntax.ParseDID(sc.Did)
		if err != nil {
			return err
		}

		topDir := cctx.String("out-dir")
		if topDir == "" {
			topDir = did.String()
		}
		log.Info("writing output", "topDir", topDir)

		commitPath := topDir + "/_commit"
		os.MkdirAll(filepath.Dir(commitPath), os.ModePerm)
		if cctx.Bool("cbor") {
			cborBytes := new(bytes.Buffer)
			err = sc.MarshalCBOR(cborBytes)
			if err := os.WriteFile(commitPath+".cbor", cborBytes.Bytes(), 0666); err != nil {
				return err
			}
		} else {
			recJson, err := json.MarshalIndent(sc, "", "  ")
			if err != nil {
				return err
			}
			if err := os.WriteFile(commitPath+".json", recJson, 0666); err != nil {
				return err
			}
		}

		err = r.ForEach(ctx, "", func(k string, v cid.Cid) error {

			_, rec, err := r.GetRecord(ctx, k)
			if err != nil {
				return err
			}
			log.Debug("processing record", "rec", k)

			// TODO: check if path is safe more carefully
			recPath := topDir + "/" + k
			os.MkdirAll(filepath.Dir(recPath), os.ModePerm)
			if err != nil {
				return err
			}
			if cctx.Bool("cbor") {
				cborBytes := new(bytes.Buffer)
				err = rec.MarshalCBOR(cborBytes)
				if err := os.WriteFile(recPath+".cbor", cborBytes.Bytes(), 0666); err != nil {
					return err
				}
			} else {
				recJson, err := json.MarshalIndent(rec, "", "  ")
				if err != nil {
					return err
				}
				if err := os.WriteFile(recPath+".json", recJson, 0666); err != nil {
					return err
				}
			}

			return nil
		})
		if err != nil {
			return err
		}

		return nil
	},
}
