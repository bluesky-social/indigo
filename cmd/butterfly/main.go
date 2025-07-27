package main

import (
	"log"
	"os"

	"github.com/urfave/cli/v2"
)

func main() {
	app := &cli.App{
		Name:  "butterfly",
		Usage: "AT Protocol data sync and discovery tool",
		Description: `Butterfly is a flexible tool for syncing and discovering AT Protocol data from various sources.
It supports fetching data from CAR files, PDS instances, relays, and Jetstream, and can output
to different storage backends.`,
		Commands: []*cli.Command{
			syncCommand,
			discoverCommand,
		},
		Version: "0.0.1",
		Authors: []*cli.Author{
			{
				Name:  "Bluesky",
				Email: "support@bsky.social",
			},
		},
	}

	if err := app.Run(os.Args); err != nil {
		log.Fatal(err)
	}
}
