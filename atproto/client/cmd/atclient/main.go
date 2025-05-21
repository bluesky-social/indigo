package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"

	"github.com/bluesky-social/indigo/atproto/client"
	"github.com/bluesky-social/indigo/atproto/identity"
	"github.com/bluesky-social/indigo/atproto/syntax"

	"github.com/urfave/cli/v2"
)

func main() {
	app := cli.App{
		Name:  "atclient",
		Usage: "dev helper for atproto/client SDK",
		Commands: []*cli.Command{
			&cli.Command{
				Name:   "get-feed-public",
				Usage:  "do a basic GET request (getAuthorFeed)",
				Action: runGetFeedPublic,
				Flags: []cli.Flag{
					&cli.StringFlag{
						Name:  "host",
						Value: "https://public.api.bsky.app",
						Usage: "service host",
					},
				},
			},
			&cli.Command{
				Name:   "login-auth",
				Usage:  "do a basic login and GET session info",
				Action: runLoginAuth,
				Flags: []cli.Flag{
					&cli.StringFlag{
						Name:     "username",
						Required: true,
						Aliases:  []string{"u"},
						Usage:    "handle or DID (not email)",
					},
					&cli.StringFlag{
						Name:     "password",
						Required: true,
						Aliases:  []string{"p"},
						Usage:    "password (or app password)",
					},
				},
			},
			&cli.Command{
				Name:   "get-feed-auth",
				Usage:  "basic authenticated GET request",
				Action: runGetFeedAuth,
				Flags: []cli.Flag{
					&cli.StringFlag{
						Name:     "username",
						Required: true,
						Aliases:  []string{"u"},
						Usage:    "handle or DID (not email)",
					},
					&cli.StringFlag{
						Name:     "password",
						Required: true,
						Aliases:  []string{"p"},
						Usage:    "password (or app password)",
					},
					&cli.StringFlag{
						Name: "labelers",
					},
					&cli.StringFlag{
						Name:  "appview",
						Value: "did:web:api.bsky.app#bsky_appview",
						Usage: "bsky appview service DID ref",
					},
				},
			},
			&cli.Command{
				Name:   "lookup-admin",
				Usage:  "basic PDS admin auth request (getAccountInfo)",
				Action: runLookupAdmin,
				Flags: []cli.Flag{
					&cli.StringFlag{
						Name:     "admin-password",
						Required: true,
						Aliases:  []string{"p"},
						Usage:    "admin auth password",
					},
					&cli.StringFlag{
						Name:     "host",
						Required: true,
						Usage:    "service host",
					},
					&cli.StringFlag{
						Name:     "did",
						Required: true,
						Usage:    "account DID to lookup",
					},
				},
			},
		},
	}
	h := slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelDebug})
	slog.SetDefault(slog.New(h))
	app.RunAndExitOnError()
}

func simpleGet(ctx context.Context, c *client.APIClient) error {
	params := map[string]any{
		"actor":       "atproto.com",
		"limit":       2,
		"includePins": false,
	}

	var d json.RawMessage
	err := c.Get(ctx, "app.bsky.feed.getAuthorFeed", params, &d)
	if err != nil {
		return err
	}

	out, err := json.MarshalIndent(d, "", "  ")
	if err != nil {
		return err
	}
	fmt.Println(string(out))
	return nil
}

func runGetFeedPublic(cctx *cli.Context) error {
	ctx := cctx.Context

	c := client.APIClient{
		Host: cctx.String("host"),
	}

	return simpleGet(ctx, &c)
}

func runLoginAuth(cctx *cli.Context) error {
	ctx := cctx.Context

	atid, err := syntax.ParseAtIdentifier(cctx.String("username"))
	if err != nil {
		return err
	}

	dir := identity.DefaultDirectory()

	c, err := client.LoginWithPassword(ctx, dir, *atid, cctx.String("password"), "")
	if err != nil {
		return err
	}

	var d json.RawMessage
	err = c.Get(ctx, "com.atproto.server.getSession", nil, &d)
	if err != nil {
		return err
	}

	out, err := json.MarshalIndent(d, "", "  ")
	if err != nil {
		return err
	}
	fmt.Println(string(out))
	return nil
}

func runGetFeedAuth(cctx *cli.Context) error {
	ctx := cctx.Context

	atid, err := syntax.ParseAtIdentifier(cctx.String("username"))
	if err != nil {
		return err
	}

	dir := identity.DefaultDirectory()

	c, err := client.LoginWithPassword(ctx, dir, *atid, cctx.String("password"), "")
	if err != nil {
		return err
	}
	c = c.WithService(cctx.String("appview"))

	return simpleGet(ctx, c)
}

func runLookupAdmin(cctx *cli.Context) error {
	ctx := cctx.Context

	c := client.NewAdminClient(cctx.String("host"), cctx.String("admin-password"))

	var d json.RawMessage
	params := map[string]any{
		"did": cctx.String("did"),
	}
	if err := c.Get(ctx, "com.atproto.admin.getAccountInfo", params, &d); err != nil {
		return err
	}

	out, err := json.MarshalIndent(d, "", "  ")
	if err != nil {
		return err
	}
	fmt.Println(string(out))
	return nil
}
