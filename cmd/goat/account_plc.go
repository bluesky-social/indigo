package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"slices"

	"github.com/gander-social/gander-indigo-sovereign/api/agnostic"
	comatproto "github.com/gander-social/gander-indigo-sovereign/api/atproto"
	"github.com/gander-social/gander-indigo-sovereign/atproto/crypto"
	"github.com/gander-social/gander-indigo-sovereign/atproto/syntax"

	"github.com/urfave/cli/v2"
)

var cmdAccountPlc = &cli.Command{
	Name:  "plc",
	Usage: "sub-commands for managing PLC DID via PDS host",
	Flags: []cli.Flag{
		&cli.StringFlag{
			Name:    "plc-host",
			Usage:   "method, hostname, and port of PLC registry",
			Value:   "https://plc.directory",
			EnvVars: []string{"ATP_PLC_HOST"},
		},
	},
	Subcommands: []*cli.Command{
		&cli.Command{
			Name:   "recommended",
			Usage:  "list recommended DID fields for current account",
			Action: runAccountPlcRecommended,
		},
		&cli.Command{
			Name:   "request-token",
			Usage:  "request a 2FA token (by email) for signing op",
			Action: runAccountPlcRequestToken,
		},
		&cli.Command{
			Name:      "sign",
			Usage:     "sign a PLC operation",
			ArgsUsage: `<json-file>`,
			Action:    runAccountPlcSign,
			Flags: []cli.Flag{
				&cli.StringFlag{
					Name:  "token",
					Usage: "2FA token for PLC operation signing request",
				},
			},
		},
		&cli.Command{
			Name:      "submit",
			Usage:     "submit a PLC operation (via PDS)",
			ArgsUsage: `<json-file>`,
			Action:    runAccountPlcSubmit,
		},
		&cli.Command{
			Name:   "current",
			Usage:  "print current PLC data for account (fetched from directory)",
			Action: runAccountPlcCurrent,
		},
		&cli.Command{
			Name:      "add-rotation-key",
			Usage:     "add a new rotation key to PLC identity (via PDS)",
			ArgsUsage: `<pubkey>`,
			Action:    runAccountPlcAddRotationKey,
			Flags: []cli.Flag{
				&cli.StringFlag{
					Name:  "token",
					Usage: "2FA token for PLC operation signing request",
				},
				&cli.BoolFlag{
					Name:  "first",
					Usage: "inserts key at the top of key list (highest priority)",
				},
			},
		},
	},
}

func runAccountPlcRecommended(cctx *cli.Context) error {
	ctx := context.Background()

	xrpcc, err := loadAuthClient(ctx)
	if err == ErrNoAuthSession {
		return fmt.Errorf("auth required, but not logged in")
	} else if err != nil {
		return err
	}

	resp, err := agnostic.IdentityGetRecommendedDidCredentials(ctx, xrpcc)
	if err != nil {
		return err
	}

	b, err := json.MarshalIndent(resp, "", "  ")
	if err != nil {
		return err
	}

	fmt.Println(string(b))
	return nil
}

func runAccountPlcRequestToken(cctx *cli.Context) error {
	ctx := context.Background()

	xrpcc, err := loadAuthClient(ctx)
	if err == ErrNoAuthSession {
		return fmt.Errorf("auth required, but not logged in")
	} else if err != nil {
		return err
	}

	err = comatproto.IdentityRequestPlcOperationSignature(ctx, xrpcc)
	if err != nil {
		return err
	}

	fmt.Println("Success; check email for token.")
	return nil
}

func runAccountPlcSign(cctx *cli.Context) error {
	ctx := context.Background()

	opPath := cctx.Args().First()
	if opPath == "" {
		return fmt.Errorf("need to provide JSON file path as an argument")
	}

	xrpcc, err := loadAuthClient(ctx)
	if err == ErrNoAuthSession {
		return fmt.Errorf("auth required, but not logged in")
	} else if err != nil {
		return err
	}

	fileBytes, err := os.ReadFile(opPath)
	if err != nil {
		return err
	}

	var body agnostic.IdentitySignPlcOperation_Input
	if err = json.Unmarshal(fileBytes, &body); err != nil {
		return fmt.Errorf("failed decoding PLC op JSON: %w", err)
	}

	token := cctx.String("token")
	if token != "" {
		body.Token = &token
	}

	resp, err := agnostic.IdentitySignPlcOperation(ctx, xrpcc, &body)
	if err != nil {
		return err
	}

	b, err := json.MarshalIndent(resp.Operation, "", "  ")
	if err != nil {
		return err
	}

	fmt.Println(string(b))
	return nil
}

func runAccountPlcSubmit(cctx *cli.Context) error {
	ctx := context.Background()

	opPath := cctx.Args().First()
	if opPath == "" {
		return fmt.Errorf("need to provide JSON file path as an argument")
	}

	xrpcc, err := loadAuthClient(ctx)
	if err == ErrNoAuthSession {
		return fmt.Errorf("auth required, but not logged in")
	} else if err != nil {
		return err
	}

	fileBytes, err := os.ReadFile(opPath)
	if err != nil {
		return err
	}

	var op json.RawMessage
	if err = json.Unmarshal(fileBytes, &op); err != nil {
		return fmt.Errorf("failed decoding PLC op JSON: %w", err)
	}

	err = agnostic.IdentitySubmitPlcOperation(ctx, xrpcc, &agnostic.IdentitySubmitPlcOperation_Input{
		Operation: &op,
	})
	if err != nil {
		return fmt.Errorf("failed submitting PLC op via PDS: %w", err)
	}

	return nil
}

func runAccountPlcCurrent(cctx *cli.Context) error {
	ctx := context.Background()

	xrpcc, err := loadAuthClient(ctx)
	if err == ErrNoAuthSession || xrpcc.Auth == nil {
		return fmt.Errorf("auth required, but not logged in")
	} else if err != nil {
		return err
	}

	did, err := syntax.ParseDID(xrpcc.Auth.Did)
	if err != nil {
		return err
	}

	plcData, err := fetchPLCData(ctx, cctx.String("plc-host"), did)
	if err != nil {
		return err
	}

	b, err := json.MarshalIndent(plcData, "", "  ")
	if err != nil {
		return err
	}
	fmt.Println(string(b))
	return nil
}

func runAccountPlcAddRotationKey(cctx *cli.Context) error {
	ctx := context.Background()

	newKeyStr := cctx.Args().First()
	if newKeyStr == "" {
		return fmt.Errorf("need to provide public key argument (as did:key)")
	}

	// check that it is a valid pubkey
	_, err := crypto.ParsePublicDIDKey(newKeyStr)
	if err != nil {
		return err
	}

	xrpcc, err := loadAuthClient(ctx)
	if err == ErrNoAuthSession {
		return fmt.Errorf("auth required, but not logged in")
	} else if err != nil {
		return err
	}

	did, err := syntax.ParseDID(xrpcc.Auth.Did)
	if err != nil {
		return err
	}

	// 1. fetch current PLC op: plc.directory/{did}/data
	plcData, err := fetchPLCData(ctx, cctx.String("plc-host"), did)
	if err != nil {
		return err
	}

	if len(plcData.RotationKeys) >= 5 {
		fmt.Println("WARNGING: already have 5 rotation keys, which is the maximum")
	}

	for _, k := range plcData.RotationKeys {
		if k == newKeyStr {
			return fmt.Errorf("key already registered as a rotation key")
		}
	}

	// 2. update data
	if cctx.Bool("first") {
		plcData.RotationKeys = slices.Insert(plcData.RotationKeys, 0, newKeyStr)
	} else {
		plcData.RotationKeys = append(plcData.RotationKeys, newKeyStr)
	}

	// 3. get data signed (using token)
	opBytes, err := json.Marshal(&plcData)
	if err != nil {
		return err
	}
	var body agnostic.IdentitySignPlcOperation_Input
	if err = json.Unmarshal(opBytes, &body); err != nil {
		return fmt.Errorf("failed decoding PLC op JSON: %w", err)
	}

	token := cctx.String("token")
	if token != "" {
		body.Token = &token
	}

	resp, err := agnostic.IdentitySignPlcOperation(ctx, xrpcc, &body)
	if err != nil {
		return err
	}

	// 4. submit signed op
	err = agnostic.IdentitySubmitPlcOperation(ctx, xrpcc, &agnostic.IdentitySubmitPlcOperation_Input{
		Operation: resp.Operation,
	})
	if err != nil {
		return fmt.Errorf("failed submitting PLC op via PDS: %w", err)
	}

	fmt.Println("Success!")
	return nil
}
