package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"

	"github.com/urfave/cli/v2"

	"github.com/bluesky-social/indigo/api/atproto"
	"github.com/bluesky-social/indigo/util/cliutil"
)

var accountCmd = &cli.Command{
	Name:  "account",
	Usage: "sub-commands for auth session and account management",
	Subcommands: []*cli.Command{
		createSessionCmd,
		newAccountCmd,
		refreshAuthTokenCmd,
		resetPasswordCmd,
		requestAccountDeletionCmd,
		deleteAccountCmd,
	},
}

var createSessionCmd = &cli.Command{
	Name:      "create-session",
	ArgsUsage: `<handle> <password>`,
	Action: func(cctx *cli.Context) error {
		xrpcc, err := cliutil.GetXrpcClient(cctx, false)
		if err != nil {
			return err
		}
		args, err := needArgs(cctx, "handle", "password")
		if err != nil {
			return err
		}
		handle, password := args[0], args[1]

		ses, err := atproto.ServerCreateSession(cctx.Context, xrpcc, &atproto.ServerCreateSession_Input{
			Identifier: handle,
			Password:   password,
		})
		if err != nil {
			return err
		}

		b, err := json.MarshalIndent(ses, "", "  ")
		if err != nil {
			return err
		}

		fmt.Println(string(b))
		return nil
	},
}

var newAccountCmd = &cli.Command{
	Name:      "new",
	ArgsUsage: `<email> <handle> <password> [inviteCode]`,
	Action: func(cctx *cli.Context) error {
		xrpcc, err := cliutil.GetXrpcClient(cctx, false)
		if err != nil {
			return err
		}

		args, err := needArgs(cctx, "email", "handle", "password")
		if err != nil {
			return err
		}
		email, handle, password := args[0], args[1], args[2]

		var invite *string
		if inv := cctx.Args().Get(3); inv != "" {
			invite = &inv
		}

		acc, err := atproto.ServerCreateAccount(cctx.Context, xrpcc, &atproto.ServerCreateAccount_Input{
			Email:      &email,
			Handle:     handle,
			InviteCode: invite,
			Password:   &password,
		})
		if err != nil {
			return err
		}

		b, err := json.MarshalIndent(acc, "", "  ")
		if err != nil {
			return err
		}

		fmt.Println(string(b))
		return nil
	},
}

var resetPasswordCmd = &cli.Command{
	Name:      "reset-password",
	ArgsUsage: `<email>`,
	Action: func(cctx *cli.Context) error {
		ctx := cctx.Context

		xrpcc, err := cliutil.GetXrpcClient(cctx, false)
		if err != nil {
			return err
		}

		args, err := needArgs(cctx, "email")
		if err != nil {
			return err
		}
		email := args[0]

		err = atproto.ServerRequestPasswordReset(ctx, xrpcc, &atproto.ServerRequestPasswordReset_Input{
			Email: email,
		})
		if err != nil {
			return err
		}

		inp := bufio.NewScanner(os.Stdin)
		fmt.Println("Enter recovery code from email:")
		inp.Scan()
		code := inp.Text()

		fmt.Println("Enter new password:")
		inp.Scan()
		npass := inp.Text()

		if err := atproto.ServerResetPassword(ctx, xrpcc, &atproto.ServerResetPassword_Input{
			Password: npass,
			Token:    code,
		}); err != nil {
			return err
		}

		return nil
	},
}

var refreshAuthTokenCmd = &cli.Command{
	Name:  "refresh-session",
	Usage: "refresh your auth token and overwrite it with new auth info",
	Action: func(cctx *cli.Context) error {
		xrpcc, err := cliutil.GetXrpcClient(cctx, true)
		if err != nil {
			return err
		}

		a := xrpcc.Auth
		a.AccessJwt = a.RefreshJwt

		ctx := cctx.Context
		nauth, err := atproto.ServerRefreshSession(ctx, xrpcc)
		if err != nil {
			return err
		}

		b, err := json.Marshal(nauth)
		if err != nil {
			return err
		}

		if err := os.WriteFile(cctx.String("auth"), b, 0600); err != nil {
			return err
		}

		return nil
	},
}

var requestAccountDeletionCmd = &cli.Command{
	Name: "request-deletion",
	Action: func(cctx *cli.Context) error {
		xrpcc, err := cliutil.GetXrpcClient(cctx, false)
		if err != nil {
			return err
		}

		err = atproto.ServerRequestAccountDelete(cctx.Context, xrpcc)
		if err != nil {
			return err
		}

		return nil
	},
}

var deleteAccountCmd = &cli.Command{
	Name:      "delete",
	Usage:     "permanently delete account",
	ArgsUsage: "<token> <password>",
	Action: func(cctx *cli.Context) error {
		xrpcc, err := cliutil.GetXrpcClient(cctx, false)
		if err != nil {
			return err
		}

		token := cctx.Args().First()
		password := cctx.Args().Get(1)

		err = atproto.ServerDeleteAccount(cctx.Context, xrpcc, &atproto.ServerDeleteAccount_Input{
			Did:      xrpcc.Auth.Did,
			Token:    token,
			Password: password,
		})
		if err != nil {
			return err
		}

		return nil
	},
}
