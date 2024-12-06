package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"

	"github.com/bluesky-social/indigo/api/agnostic"
	comatproto "github.com/bluesky-social/indigo/api/atproto"

	"github.com/urfave/cli/v2"
)

var cmdAccountPlc = &cli.Command{
	Name:  "plc",
	Usage: "sub-commands for managing PLC DID via PDS host",
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
					Usage: "2FA token for signing request",
				},
			},
		},
		&cli.Command{
			Name:      "submit",
			Usage:     "submit a PLC operation (via PDS)",
			ArgsUsage: `<json-file>`,
			Action:    runAccountPlcSubmit,
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
