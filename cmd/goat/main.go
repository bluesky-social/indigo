package main

import (
	"fmt"
	"os"

	_ "github.com/joho/godotenv/autoload"

	"github.com/carlmjohnson/versioninfo"
	"github.com/urfave/cli/v2"
)

func main() {
	if err := run(os.Args); err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(-1)
	}
}

var authFlags = []cli.Flag{
	&cli.StringFlag{
		Name:    "pds-host",
		Usage:   "method, hostname, and port of PDS instance",
		Value:   "http://localhost:4849",
		EnvVars: []string{"ATP_PDS_HOST"},
	},
	&cli.StringFlag{
		Name:     "admin-password",
		Usage:    "admin authentication password for PDS",
		Required: true,
		EnvVars:  []string{"ATP_AUTH_ADMIN_PASSWORD"},
	},
	&cli.StringFlag{
		Name:     "handle",
		Usage:    "for PDS login",
		Required: true,
		EnvVars:  []string{"ATP_AUTH_HANDLE"},
	},
	&cli.StringFlag{
		Name:     "password",
		Usage:    "for PDS login",
		Required: true,
		EnvVars:  []string{"ATP_AUTH_PASSWORD"},
	},
}

func run(args []string) error {

	app := cli.App{
		Name:    "goat",
		Usage:   "golang atproto CLI tool",
		Version: versioninfo.Short(),
	}
	app.Commands = []*cli.Command{
		cmdGet,
		cmdResolve,
		cmdPLC,
		cmdLogin,
		cmdStatus,
		cmdRepo,
		cmdBlob,
	}
	return app.Run(args)
}
