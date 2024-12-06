package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/bluesky-social/indigo/api/agnostic"
	comatproto "github.com/bluesky-social/indigo/api/atproto"
	"github.com/bluesky-social/indigo/atproto/syntax"
	"github.com/bluesky-social/indigo/xrpc"

	"github.com/urfave/cli/v2"
)

var cmdAccountMigrate = &cli.Command{
	Name:  "migrate",
	Usage: "move account to a new PDS. requires full auth.",
	Flags: []cli.Flag{
		&cli.StringFlag{
			Name:     "pds-host",
			Usage:    "URL of the new PDS to create account on",
			Required: true,
			EnvVars:  []string{"ATP_PDS_HOST"},
		},
		&cli.StringFlag{
			Name:     "new-handle",
			Required: true,
			Usage:    "handle on new PDS",
			EnvVars:  []string{"NEW_ACCOUNT_HANDLE"},
		},
		&cli.StringFlag{
			Name:     "new-password",
			Required: true,
			Usage:    "password on new PDS",
			EnvVars:  []string{"NEW_ACCOUNT_PASSWORD"},
		},
		&cli.StringFlag{
			Name:     "plc-token",
			Required: true,
			Usage:    "token from old PDS authorizing token signature",
			EnvVars:  []string{"PLC_SIGN_TOKEN"},
		},
		&cli.StringFlag{
			Name:  "invite-code",
			Usage: "invite code for account signup",
		},
		&cli.StringFlag{
			Name:  "new-email",
			Usage: "email address for new account",
		},
	},
	Action: runAccountMigrate,
}

func runAccountMigrate(cctx *cli.Context) error {
	// NOTE: this could check rev / commit before and after and ensure last-minute content additions get lost
	ctx := context.Background()

	oldClient, err := loadAuthClient(ctx)
	if err == ErrNoAuthSession {
		return fmt.Errorf("auth required, but not logged in")
	} else if err != nil {
		return err
	}
	did := oldClient.Auth.Did

	newHostURL := cctx.String("pds-host")
	if !strings.Contains(newHostURL, "://") {
		return fmt.Errorf("PDS host is not a url: %s", newHostURL)
	}
	newHandle := cctx.String("new-handle")
	_, err = syntax.ParseHandle(newHandle)
	if err != nil {
		return err
	}
	newPassword := cctx.String("new-password")
	plcToken := cctx.String("plc-token")
	inviteCode := cctx.String("invite-code")
	newEmail := cctx.String("new-email")

	newClient := xrpc.Client{
		Host: newHostURL,
	}

	// connect to new host to discover service DID
	newHostDesc, err := comatproto.ServerDescribeServer(ctx, &newClient)
	if err != nil {
		return fmt.Errorf("failed connecting to new host: %w", err)
	}
	newHostDID, err := syntax.ParseDID(newHostDesc.Did)
	if err != nil {
		return err
	}
	slog.Info("new host", "serviceDID", newHostDID, "url", newHostURL)

	// 1. Create New Account
	slog.Info("creating account on new host", "handle", newHandle, "host", newHostURL)

	// get service auth token from old host
	// args: (ctx, client, aud string, exp int64, lxm string)
	expTimestamp := time.Now().Unix() + 60
	createAuthResp, err := comatproto.ServerGetServiceAuth(ctx, oldClient, newHostDID.String(), expTimestamp, "com.atproto.server.createAccount")
	if err != nil {
		return fmt.Errorf("failed getting service auth token from old host: %w", err)
	}

	// then create the new account
	createParams := comatproto.ServerCreateAccount_Input{
		Did:      &did,
		Handle:   newHandle,
		Password: &newPassword,
	}
	if newEmail != "" {
		createParams.Email = &newEmail
	}
	if inviteCode != "" {
		createParams.InviteCode = &inviteCode
	}

	// use service auth for access token, temporarily
	newClient.Auth = &xrpc.AuthInfo{
		Did:        did,
		Handle:     newHandle,
		AccessJwt:  createAuthResp.Token,
		RefreshJwt: createAuthResp.Token,
	}
	createAccountResp, err := comatproto.ServerCreateAccount(ctx, &newClient, &createParams)
	if err != nil {
		return fmt.Errorf("failed creating new account: %w", err)
	}

	if createAccountResp.Did != did {
		return fmt.Errorf("new account DID not a match: %s != %s", createAccountResp.Did, did)
	}
	newClient.Auth.AccessJwt = createAccountResp.AccessJwt
	newClient.Auth.RefreshJwt = createAccountResp.RefreshJwt

	// login client on the new host
	sess, err := comatproto.ServerCreateSession(ctx, &newClient, &comatproto.ServerCreateSession_Input{
		Identifier: did,
		Password:   newPassword,
	})
	if err != nil {
		return fmt.Errorf("failed login to newly created account on new host: %w", err)
	}
	newClient.Auth = &xrpc.AuthInfo{
		Did:        did,
		AccessJwt:  sess.AccessJwt,
		RefreshJwt: sess.RefreshJwt,
	}

	// 2. Migrate Data
	slog.Info("migrating repo")
	repoBytes, err := comatproto.SyncGetRepo(ctx, oldClient, did, "")
	if err != nil {
		return fmt.Errorf("failed exporting repo: %w", err)
	}
	err = comatproto.RepoImportRepo(ctx, &newClient, bytes.NewReader(repoBytes))
	if err != nil {
		return fmt.Errorf("failed importing repo: %w", err)
	}

	slog.Info("migrating preferences")
	// TODO: service proxy header for AppView?
	prefResp, err := agnostic.ActorGetPreferences(ctx, oldClient)
	if err != nil {
		return fmt.Errorf("failed fetching old preferences: %w", err)
	}
	err = agnostic.ActorPutPreferences(ctx, &newClient, &agnostic.ActorPutPreferences_Input{
		Preferences: prefResp.Preferences,
	})
	if err != nil {
		return fmt.Errorf("failed importing preferences: %w", err)
	}

	slog.Info("migrating blobs")
	blobCursor := ""
	for {
		listResp, err := comatproto.SyncListBlobs(ctx, oldClient, blobCursor, did, 100, "")
		if err != nil {
			return fmt.Errorf("failed listing blobs: %w", err)
		}
		for _, blobCID := range listResp.Cids {
			blobBytes, err := comatproto.SyncGetBlob(ctx, oldClient, blobCID, did)
			if err != nil {
				slog.Warn("failed downloading blob", "cid", blobCID, "err", err)
				continue
			}
			_, err = comatproto.RepoUploadBlob(ctx, &newClient, bytes.NewReader(blobBytes))
			if err != nil {
				slog.Warn("failed uploading blob", "cid", blobCID, "err", err, "size", len(blobBytes))
			}
			slog.Info("transferred blob", "cid", blobCID, "size", len(blobBytes))
		}
		if listResp.Cursor == nil || *listResp.Cursor == "" {
			break
		}
		blobCursor = *listResp.Cursor
	}

	// display migration status
	// NOTE: this could check between the old PDS and new PDS, polling in a loop showing progress until all records have been indexed
	statusResp, err := comatproto.ServerCheckAccountStatus(ctx, &newClient)
	if err != nil {
		return fmt.Errorf("failed checking account status: %w", err)
	}
	slog.Info("account migration status", "status", statusResp)

	// 3. Migrate Identity
	// NOTE: to work with did:web or non-PDS-managed did:plc, need to do manual migraiton process
	slog.Info("updating identity to new host")

	credsResp, err := agnostic.IdentityGetRecommendedDidCredentials(ctx, &newClient)
	if err != nil {
		return fmt.Errorf("failed fetching new credentials: %w", err)
	}
	credsBytes, err := json.Marshal(credsResp)
	if err != nil {
		return nil
	}

	var unsignedOp agnostic.IdentitySignPlcOperation_Input
	if err = json.Unmarshal(credsBytes, &unsignedOp); err != nil {
		return fmt.Errorf("failed parsing PLC op: %w", err)
	}
	unsignedOp.Token = &plcToken

	// NOTE: could add additional sanity checks here that any extra rotation keys were retained, and that old alsoKnownAs and service entries are retained? The stakes aren't super high for the later, as PLC has the full history. PLC and the new PDS already implement some basic sanity checks.

	signedPlcOpResp, err := agnostic.IdentitySignPlcOperation(ctx, oldClient, &unsignedOp)
	if err != nil {
		return fmt.Errorf("failed requesting PLC operation signature: %w", err)
	}

	err = agnostic.IdentitySubmitPlcOperation(ctx, &newClient, &agnostic.IdentitySubmitPlcOperation_Input{
		Operation: signedPlcOpResp.Operation,
	})
	if err != nil {
		return fmt.Errorf("failed submitting PLC operation: %w", err)
	}

	// 4. Finalize Migration
	slog.Info("activating new account")

	err = comatproto.ServerActivateAccount(ctx, &newClient)
	if err != nil {
		return fmt.Errorf("failed activating new host: %w", err)
	}
	err = comatproto.ServerDeactivateAccount(ctx, oldClient, &comatproto.ServerDeactivateAccount_Input{})
	if err != nil {
		return fmt.Errorf("failed deactivating old host: %w", err)
	}

	slog.Info("account migration completed")
	return nil
}
