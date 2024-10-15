package main

import (
	"fmt"
	slogging "log/slog"
	"os"

	"github.com/carlmjohnson/versioninfo"
	"github.com/urfave/cli/v2"

	_ "github.com/joho/godotenv/autoload"
)

var (
	slog    = slogging.New(slogging.NewJSONHandler(os.Stdout, nil))
	version = versioninfo.Short()
)

func main() {
	if err := run(os.Args); err != nil {
		slog.Error("fatal", "err", err)
		os.Exit(-1)
	}
}

func run(args []string) error {

	app := cli.App{
		Name:  "astrolabe",
		Usage: "public web interface to explore atproto network content",
	}

	app.Commands = []*cli.Command{
		&cli.Command{
			Name:   "serve",
			Usage:  "run the server",
			Action: serve,
			Flags: []cli.Flag{
				&cli.StringFlag{
					Name:     "bind",
					Usage:    "Specify the local IP/port to bind to",
					Required: false,
					Value:    ":8400",
					EnvVars:  []string{"ASTROLABE_BIND"},
				},
				&cli.BoolFlag{
					Name:     "debug",
					Usage:    "Enable debug mode",
					Value:    false,
					Required: false,
					EnvVars:  []string{"DEBUG"},
				},
			},
		},
		&cli.Command{
			Name:  "version",
			Usage: "print version",
			Action: func(cctx *cli.Context) error {
				fmt.Println(version)
				return nil
			},
		},
	}

	return app.Run(args)
}
