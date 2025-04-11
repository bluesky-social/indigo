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
				Name:   "get",
				Usage:  "do a basic GET request",
				Action: runGet,
			},
			&cli.Command{
				Name:   "login-refresh",
				Usage:  "do a basic login and GET request",
				Action: runLoginRefresh,
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
		},
	}
	h := slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelDebug})
	slog.SetDefault(slog.New(h))
	app.RunAndExitOnError()
}

func simpleGet(ctx context.Context, c *client.APIClient) error {
	params := map[string]string{
		"actor":       "atproto.com",
		"limit":       "5",
		"includePins": "false",
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

func runGet(cctx *cli.Context) error {
	ctx := context.Background()

	c := client.APIClient{
		Host: "https://public.api.bsky.app",
	}

	return simpleGet(ctx, &c)
}

func runLoginRefresh(cctx *cli.Context) error {
	ctx := context.Background()

	atid, err := syntax.ParseAtIdentifier(cctx.String("username"))
	if err != nil {
		return err
	}

	dir := identity.DefaultDirectory()
	ident, err := dir.Lookup(ctx, *atid)
	if err != nil {
		return err
	}

	c := client.APIClient{
		Host: ident.PDSEndpoint(),
	}

	_, err = client.NewSession(ctx, &c, atid.String(), cctx.String("password"), "")
	if err != nil {
		return err
	}

	return simpleGet(ctx, &c)
}
