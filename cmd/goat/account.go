package main

import (
	"context"
	"encoding/json"
	"fmt"

	comatproto "github.com/bluesky-social/indigo/api/atproto"
	"github.com/bluesky-social/indigo/atproto/syntax"
	"github.com/bluesky-social/indigo/xrpc"

	"github.com/urfave/cli/v2"
)

var cmdAccount = &cli.Command{
	Name:  "account",
	Usage: "sub-commands for auth and account management",
	Flags: []cli.Flag{},
	Subcommands: []*cli.Command{
		&cli.Command{
			Name:   "check",
			Usage:  "verifies current auth session is functional",
			Action: runAccountCheck,
		},
		&cli.Command{
			Name:  "login",
			Usage: "create session with PDS instance",
			Flags: []cli.Flag{
				&cli.StringFlag{
					Name:     "username",
					Aliases:  []string{"u"},
					Required: true,
					Usage:    "account identifier (handle or DID)",
					EnvVars:  []string{"ATP_AUTH_USERNAME"},
				},
				&cli.StringFlag{
					Name:     "app-password",
					Aliases:  []string{"p"},
					Required: true,
					Usage:    "password (app password recommended)",
					EnvVars:  []string{"ATP_AUTH_PASSWORD"},
				},
			},
			Action: runAccountLogin,
		},
		&cli.Command{
			Name:   "logout",
			Usage:  "delete any current session",
			Action: runAccountLogout,
		},
		&cli.Command{
			Name:      "lookup",
			Usage:     "show basic account status for any account",
			ArgsUsage: `<at-identifier>`,
			Action:    runAccountLookup,
		},
		&cli.Command{
			Name:   "status",
			Usage:  "show current account status at PDS",
			Action: runAccountStatus,
		},
		cmdAccountMigrate,
	},
}

func runAccountCheck(cctx *cli.Context) error {
	ctx := context.Background()

	client, err := loadAuthClient(ctx)
	if err == ErrNoAuthSession {
		return fmt.Errorf("auth required, but not logged in")
	} else if err != nil {
		return err
	}
	// TODO: more explicit check?
	fmt.Printf("DID: %s\n", client.Auth.Did)
	fmt.Printf("PDS: %s\n", client.Host)

	return nil
}

func runAccountLogin(cctx *cli.Context) error {
	ctx := context.Background()

	username, err := syntax.ParseAtIdentifier(cctx.String("username"))
	if err != nil {
		return err
	}

	_, err = refreshAuthSession(ctx, *username, cctx.String("app-password"))
	return err
}

func runAccountLogout(cctx *cli.Context) error {
	return wipeAuthSession()
}

func runAccountLookup(cctx *cli.Context) error {
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

	status, err := comatproto.SyncGetRepoStatus(ctx, &xrpcc, ident.DID.String())
	if err != nil {
		return err
	}

	fmt.Printf("DID: %s\n", status.Did)
	fmt.Printf("Active: %v\n", status.Active)
	if status.Status != nil {
		fmt.Printf("Status: %s\n", *status.Status)
	}
	if status.Rev != nil {
		fmt.Printf("Repo Rev: %s\n", *status.Rev)
	}
	return nil
}

func runAccountStatus(cctx *cli.Context) error {
	ctx := context.Background()

	client, err := loadAuthClient(ctx)
	if err == ErrNoAuthSession {
		return fmt.Errorf("auth required, but not logged in")
	} else if err != nil {
		return err
	}

	status, err := comatproto.ServerCheckAccountStatus(ctx, client)
	if err != nil {
		return fmt.Errorf("failed checking account status: %w", err)
	}

	b, err := json.MarshalIndent(status, "", "  ")
	if err != nil {
		return err
	}
	fmt.Println(string(b))

	return nil
}
