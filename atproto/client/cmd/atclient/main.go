package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"

	"github.com/bluesky-social/indigo/atproto/client"

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
		},
	}
	h := slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelDebug})
	slog.SetDefault(slog.New(h))
	app.RunAndExitOnError()
}

func runGet(cctx *cli.Context) error {
	ctx := context.Background()

	c := client.BaseAPIClient{
		Host: "https://public.api.bsky.app",
	}

	params := map[string]string{
		"actor":       "atproto.com",
		"limit":       "5",
		"includePins": "false",
	}
	b, err := c.Get(ctx, "app.bsky.feed.getAuthorFeed", params)
	if err != nil {
		return err
	}

	out, err := json.MarshalIndent(b, "", "  ")
	if err != nil {
		return err
	}
	fmt.Println(string(out))
	return nil
}
