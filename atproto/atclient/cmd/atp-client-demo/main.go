package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"

	comatproto "github.com/bluesky-social/indigo/api/agnostic"
	"github.com/bluesky-social/indigo/atproto/atclient"
	"github.com/bluesky-social/indigo/atproto/identity"
	"github.com/bluesky-social/indigo/atproto/syntax"

	"github.com/urfave/cli/v3"
)

func main() {
	app := cli.Command{
		Name:  "atp-client-demo",
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
				Name:   "list-records-public",
				Usage:  "do a basic GET request (listRecords)",
				Action: runListRecordsPublic,
				Flags: []cli.Flag{
					&cli.StringFlag{
						Name:  "host",
						Value: "https://enoki.us-east.host.bsky.network",
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
	if err := app.Run(context.Background(), os.Args); err != nil {
		slog.Error("command failed", "error", err)
		os.Exit(-1)
	}
}

func getFeed(ctx context.Context, c *atclient.APIClient) error {
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

func listRecords(ctx context.Context, c *atclient.APIClient) error {

	list, err := comatproto.RepoListRecords(ctx, c, "app.bsky.actor.profile", "", 10, "did:plc:ewvi7nxzyoun6zhxrhs64oiz", false)
	if err != nil {
		return err
	}

	out, err := json.MarshalIndent(list, "", "  ")
	if err != nil {
		return err
	}
	fmt.Println(string(out))
	return nil
}

func runGetFeedPublic(ctx context.Context, cmd *cli.Command) error {

	c := atclient.APIClient{
		Host: cmd.String("host"),
	}

	return getFeed(ctx, &c)
}

func runListRecordsPublic(ctx context.Context, cmd *cli.Command) error {

	c := atclient.APIClient{
		Host: cmd.String("host"),
	}

	return listRecords(ctx, &c)
}

func runLoginAuth(ctx context.Context, cmd *cli.Command) error {

	atid, err := syntax.ParseAtIdentifier(cmd.String("username"))
	if err != nil {
		return err
	}

	dir := identity.DefaultDirectory()

	c, err := atclient.LoginWithPassword(ctx, dir, *atid, cmd.String("password"), "", nil)
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

func runGetFeedAuth(ctx context.Context, cmd *cli.Command) error {

	atid, err := syntax.ParseAtIdentifier(cmd.String("username"))
	if err != nil {
		return err
	}

	dir := identity.DefaultDirectory()

	c, err := atclient.LoginWithPassword(ctx, dir, *atid, cmd.String("password"), "", nil)
	if err != nil {
		return err
	}
	c = c.WithService(cmd.String("appview"))

	return getFeed(ctx, c)
}

func runLookupAdmin(ctx context.Context, cmd *cli.Command) error {

	c := atclient.NewAdminClient(cmd.String("host"), cmd.String("admin-password"))

	var d json.RawMessage
	params := map[string]any{
		"did": cmd.String("did"),
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
