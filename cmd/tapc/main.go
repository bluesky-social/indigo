package main

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"os"

	"github.com/bluesky-social/indigo/internal/websocket"
	"github.com/earthboundkid/versioninfo/v2"
	"github.com/urfave/cli/v3"
)

func main() {
	if err := run(os.Args); err != nil {
		slog.Error("exiting process", "error", err)
		os.Exit(-1)
	}
}

func run(args []string) error {
	app := &cli.Command{
		Name:    "tapc",
		Usage:   "atproto client tool",
		Version: versioninfo.Short(),
		Commands: []*cli.Command{
			{
				Name:  "connect",
				Usage: "connect to a TAP server",
				Flags: []cli.Flag{
					&cli.StringFlag{
						Name:  "url",
						Usage: "TAP server url",
						Value: "ws://localhost:2480/channel",
					},
				},
				Action: runTapc,
			},
		},
	}
	return app.Run(context.Background(), args)
}

func runTapc(ctx context.Context, cmd *cli.Command) error {
	url := cmd.String("url")

	slog.Info("connecting to TAP server", "url", url)

	c := websocket.NewClient(ctx, slog.Default(), url, fmt.Sprintf("tapc/%s", versioninfo.Short()))
	defer c.Close()

	for c.Next() {
		typ, r := c.Message()
		switch typ {
		case websocket.TextMessage:
			body, err := io.ReadAll(r)
			if err != nil {
				return fmt.Errorf("failed to read text message: %w", err)
			}
			fmt.Printf("%s\n", string(body))
		default:
			// ¯\_(ツ)_/¯
		}
	}
	return c.Err()
}
