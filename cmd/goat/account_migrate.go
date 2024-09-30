package main

import (
	"bytes"
	"context"
	"fmt"
	"log/slog"
	"time"

	comatproto "github.com/bluesky-social/indigo/api/atproto"
	appbsky "github.com/bluesky-social/indigo/api/bsky"
	"github.com/bluesky-social/indigo/atproto/syntax"
	"github.com/bluesky-social/indigo/xrpc"

	"github.com/urfave/cli/v2"
)

var cmdAccountMigrate = &cli.Command{
	Name:  "migrate",
	Usage: "move account to a new PDS. requires full auth.",
	Flags: []cli.Flag{
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
	},
	Action: runAccountMigrate,
}

func runAccountMigrate(cctx *cli.Context) error {
	// TODO: this could check rev / commit before and after
	ctx := context.Background()

	oldClient, err := loadAuthClient(ctx)
	if err == ErrNoAuthSession {
		return fmt.Errorf("auth required, but not logged in")
	} else if err != nil {
		return err
	}
	did := oldClient.Auth.Did

	// connect to new host to discover service DID
	newHostURL := "XXX"
	newHandle := "XXX"
	newPassword := "XXX"
	newEmail := "XXX"
	inviteCode := "XXX"
	newClient := xrpc.Client{
		Host: newHostURL,
	}
	newHostDesc, err := comatproto.ServerDescribeServer(ctx, &newClient)
	if err != nil {
		return fmt.Errorf("failed connecting to new host: %w", err)
	}
	newHostDID, err := syntax.ParseDID(newHostDesc.Did)
	if err != nil {
		return err
	}
	slog.Info("new host", "did", newHostDID, "url", newHostURL)

	// 1. Create New Account
	slog.Info("creating account on new host", "handle", newHandle)

	// get service auth token from old host
	// args: (ctx, client, aud string, exp int64, lxm string)
	expTimestamp := time.Now().Unix() + 60
	createAuthResp, err := comatproto.ServerGetServiceAuth(ctx, oldClient, newHostDID.String(), expTimestamp, "com.atproto.server.createAccount")
	if err != nil {
		return fmt.Errorf("failed getting service auth token from old host: %w", err)
	}

	// then create the new account
	// XXX: "Authorization  Bearer <createAuthResp.Token>"
	_ = createAuthResp
	createAccountResp, err := comatproto.ServerCreateAccount(ctx, &newClient, &comatproto.ServerCreateAccount_Input{
		Did:        &did,
		Email:      &newEmail,
		Handle:     newHandle,
		InviteCode: &inviteCode,
		Password:   &newPassword,
	})
	if err != nil {
		return fmt.Errorf("failed getting service auth token from old host: %w", err)
	}
	// TODO: validate response?
	_ = createAccountResp

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
	// TODO: service proxy header
	prefResp, err := appbsky.ActorGetPreferences(ctx, oldClient)
	if err != nil {
		return fmt.Errorf("failed fetching old preferences: %w", err)
	}
	err = appbsky.ActorPutPreferences(ctx, &newClient, &appbsky.ActorPutPreferences_Input{
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
			slog.Info("transfered blob", "cid", blobCID, "size", len(blobBytes))
		}
		if listResp.Cursor == nil || *listResp.Cursor == "" {
			break
		}
		blobCursor = *listResp.Cursor
	}

	// display migration status
	// TODO: should this loop? with a delay?
	statusResp, err := comatproto.ServerCheckAccountStatus(ctx, &newClient)
	if err != nil {
		return fmt.Errorf("failed checking account status: %w", err)
	}
	slog.Info("account migration status", "status", statusResp)

	// 3. Migrate Identity
	// TODO: support did:web etc
	slog.Info("updating identity to new host")

	credsResp, err := comatproto.IdentityGetRecommendedDidCredentials(ctx, &newClient)
	if err != nil {
		return fmt.Errorf("failed fetching new credentials: %w", err)
	}

	// TODO: add some safety checks here around rotation key set intersection?

	err = comatproto.IdentityRequestPlcOperationSignature(ctx, oldClient)
	if err != nil {
		return fmt.Errorf("failed requesting PLC operation token: %w", err)
	}

	plcAuthToken := "XXX"
	signedPlcOpResp, err := comatproto.IdentitySignPlcOperation(ctx, oldClient, &comatproto.IdentitySignPlcOperation_Input{
		// XXX: not just pass-through
		AlsoKnownAs:         credsResp.AlsoKnownAs,
		RotationKeys:        credsResp.RotationKeys,
		Services:            credsResp.Services,
		Token:               &plcAuthToken,
		VerificationMethods: credsResp.VerificationMethods,
	})
	if err != nil {
		return fmt.Errorf("failed requesting PLC operation signature: %w", err)
	}

	err = comatproto.IdentitySubmitPlcOperation(ctx, &newClient, &comatproto.IdentitySubmitPlcOperation_Input{
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
	err = comatproto.ServerDeactivateAccount(ctx, oldClient, nil)
	if err != nil {
		return fmt.Errorf("failed deactivating old host: %w", err)
	}

	slog.Info("account migration completed")
	return nil
}
