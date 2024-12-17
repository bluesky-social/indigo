package main

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

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
				&cli.StringFlag{
					Name:    "auth-factor-token",
					Usage:   "token required if password is used and 2fa is required",
					EnvVars: []string{"ATP_AUTH_FACTOR_TOKEN"},
				},
				&cli.StringFlag{
					Name:    "pds-host",
					Usage:   "URL of the PDS to create account on (overrides DID doc)",
					EnvVars: []string{"ATP_PDS_HOST"},
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
			Name:   "activate",
			Usage:  "(re)activate current account",
			Action: runAccountActivate,
		},
		&cli.Command{
			Name:   "deactivate",
			Usage:  "deactivate current account",
			Action: runAccountDeactivate,
		},
		&cli.Command{
			Name:      "lookup",
			Usage:     "show basic account hosting status for any account",
			ArgsUsage: `<at-identifier>`,
			Action:    runAccountLookup,
		},
		&cli.Command{
			Name:      "update-handle",
			Usage:     "change handle for current account",
			ArgsUsage: `<handle>`,
			Action:    runAccountUpdateHandle,
		},
		&cli.Command{
			Name:   "status",
			Usage:  "show current account status at PDS",
			Action: runAccountStatus,
		},
		&cli.Command{
			Name:   "missing-blobs",
			Usage:  "list any missing blobs for current account",
			Action: runAccountMissingBlobs,
		},
		&cli.Command{
			Name:  "service-auth",
			Usage: "create service auth token",
			Flags: []cli.Flag{
				&cli.StringFlag{
					Name:    "endpoint",
					Aliases: []string{"lxm"},
					Usage:   "restrict token to API endpoint (NSID, optional)",
				},
				&cli.StringFlag{
					Name:     "audience",
					Aliases:  []string{"aud"},
					Required: true,
					Usage:    "DID of service that will receive and validate token",
				},
				&cli.IntFlag{
					Name:  "duration-sec",
					Value: 60,
					Usage: "validity time window of token (seconds)",
				},
			},
			Action: runAccountServiceAuth,
		},
		&cli.Command{
			Name:  "create",
			Usage: "create a new account on the indicated PDS host",
			Flags: []cli.Flag{
				&cli.StringFlag{
					Name:     "pds-host",
					Usage:    "URL of the PDS to create account on",
					Required: true,
					EnvVars:  []string{"ATP_PDS_HOST"},
				},
				&cli.StringFlag{
					Name:     "handle",
					Usage:    "handle for new account",
					Required: true,
					EnvVars:  []string{"ATP_AUTH_HANDLE"},
				},
				&cli.StringFlag{
					Name:     "password",
					Usage:    "initial account password",
					Required: true,
					EnvVars:  []string{"ATP_AUTH_PASSWORD"},
				},
				&cli.StringFlag{
					Name:  "invite-code",
					Usage: "invite code for account signup",
				},
				&cli.StringFlag{
					Name:  "email",
					Usage: "email address for new account",
				},
				&cli.StringFlag{
					Name:  "existing-did",
					Usage: "an existing DID to use (eg, non-PLC DID, or migration)",
				},
				&cli.StringFlag{
					Name:  "recovery-key",
					Usage: "public cryptographic key (did:key) to add as PLC recovery",
				},
				&cli.StringFlag{
					Name:  "service-auth",
					Usage: "service auth token (for account migration)",
				},
			},
			Action: runAccountCreate,
		},
		cmdAccountMigrate,
		cmdAccountPlc,
	},
}

func runAccountLogin(cctx *cli.Context) error {
	ctx := context.Background()

	username, err := syntax.ParseAtIdentifier(cctx.String("username"))
	if err != nil {
		return err
	}

	_, err = refreshAuthSession(ctx, *username, cctx.String("app-password"), cctx.String("pds-host"), cctx.String("auth-factor-token"))
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
	fmt.Printf("DID: %s\n", client.Auth.Did)
	fmt.Printf("Host: %s\n", client.Host)
	fmt.Println(string(b))

	return nil
}

func runAccountMissingBlobs(cctx *cli.Context) error {
	ctx := context.Background()

	client, err := loadAuthClient(ctx)
	if err == ErrNoAuthSession {
		return fmt.Errorf("auth required, but not logged in")
	} else if err != nil {
		return err
	}

	cursor := ""
	for {
		resp, err := comatproto.RepoListMissingBlobs(ctx, client, cursor, 500)
		if err != nil {
			return err
		}
		for _, missing := range resp.Blobs {
			fmt.Printf("%s\t%s\n", missing.Cid, missing.RecordUri)
		}
		if resp.Cursor != nil && *resp.Cursor != "" {
			cursor = *resp.Cursor
		} else {
			break
		}
	}
	return nil
}

func runAccountActivate(cctx *cli.Context) error {
	ctx := context.Background()

	client, err := loadAuthClient(ctx)
	if err == ErrNoAuthSession {
		return fmt.Errorf("auth required, but not logged in")
	} else if err != nil {
		return err
	}

	err = comatproto.ServerActivateAccount(ctx, client)
	if err != nil {
		return fmt.Errorf("failed activating account: %w", err)
	}

	return nil
}

func runAccountDeactivate(cctx *cli.Context) error {
	ctx := context.Background()

	client, err := loadAuthClient(ctx)
	if err == ErrNoAuthSession {
		return fmt.Errorf("auth required, but not logged in")
	} else if err != nil {
		return err
	}

	err = comatproto.ServerDeactivateAccount(ctx, client, &comatproto.ServerDeactivateAccount_Input{})
	if err != nil {
		return fmt.Errorf("failed deactivating account: %w", err)
	}

	return nil
}

func runAccountUpdateHandle(cctx *cli.Context) error {
	ctx := context.Background()

	raw := cctx.Args().First()
	if raw == "" {
		return fmt.Errorf("need to provide new handle as argument")
	}
	handle, err := syntax.ParseHandle(raw)
	if err != nil {
		return err
	}

	client, err := loadAuthClient(ctx)
	if err == ErrNoAuthSession {
		return fmt.Errorf("auth required, but not logged in")
	} else if err != nil {
		return err
	}

	err = comatproto.IdentityUpdateHandle(ctx, client, &comatproto.IdentityUpdateHandle_Input{
		Handle: handle.String(),
	})
	if err != nil {
		return fmt.Errorf("failed updating handle: %w", err)
	}

	return nil
}

func runAccountServiceAuth(cctx *cli.Context) error {
	ctx := context.Background()

	client, err := loadAuthClient(ctx)
	if err == ErrNoAuthSession {
		return fmt.Errorf("auth required, but not logged in")
	} else if err != nil {
		return err
	}

	lxm := cctx.String("endpoint")
	if lxm != "" {
		_, err := syntax.ParseNSID(lxm)
		if err != nil {
			return fmt.Errorf("lxm argument must be a valid NSID: %w", err)
		}
	}

	aud := cctx.String("audience")
	// TODO: can aud DID have a fragment?
	_, err = syntax.ParseDID(aud)
	if err != nil {
		return fmt.Errorf("aud argument must be a valid DID: %w", err)
	}

	durSec := cctx.Int("duration-sec")
	expTimestamp := time.Now().Unix() + int64(durSec)

	resp, err := comatproto.ServerGetServiceAuth(ctx, client, aud, expTimestamp, lxm)
	if err != nil {
		return fmt.Errorf("failed updating handle: %w", err)
	}

	fmt.Println(resp.Token)

	return nil
}

func runAccountCreate(cctx *cli.Context) error {
	ctx := context.Background()

	// validate args
	pdsHost := cctx.String("pds-host")
	if !strings.Contains(pdsHost, "://") {
		return fmt.Errorf("PDS host is not a url: %s", pdsHost)
	}
	handle := cctx.String("handle")
	_, err := syntax.ParseHandle(handle)
	if err != nil {
		return err
	}
	password := cctx.String("password")
	params := &comatproto.ServerCreateAccount_Input{
		Handle:   handle,
		Password: &password,
	}
	raw := cctx.String("existing-did")
	if raw != "" {
		_, err := syntax.ParseDID(raw)
		if err != nil {
			return err
		}
		s := raw
		params.Did = &s
	}
	raw = cctx.String("email")
	if raw != "" {
		s := raw
		params.Email = &s
	}
	raw = cctx.String("invite-code")
	if raw != "" {
		s := raw
		params.InviteCode = &s
	}
	raw = cctx.String("recovery-key")
	if raw != "" {
		s := raw
		params.RecoveryKey = &s
	}

	// create a new API client to connect to the account's PDS
	xrpcc := xrpc.Client{
		Host: pdsHost,
	}

	raw = cctx.String("service-auth")
	if raw != "" && params.Did != nil {
		xrpcc.Auth = &xrpc.AuthInfo{
			Did:       *params.Did,
			AccessJwt: raw,
		}
	}

	resp, err := comatproto.ServerCreateAccount(ctx, &xrpcc, params)
	if err != nil {
		return fmt.Errorf("failed to create account: %w", err)
	}

	fmt.Println("Success!")
	fmt.Printf("DID: %s\n", resp.Did)
	fmt.Printf("Handle: %s\n", resp.Handle)
	return nil
}
