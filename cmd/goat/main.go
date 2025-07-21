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

func run(args []string) error {

	app := cli.App{
		Name:    "goat",
		Usage:   "Go AT protocol CLI tool",
		Version: versioninfo.Short(),
		Flags: []cli.Flag{
			&cli.StringFlag{
				Name:    "log-level",
				Usage:   "log verbosity level (eg: warn, info, debug)",
				EnvVars: []string{"GOAT_LOG_LEVEL", "GO_LOG_LEVEL", "LOG_LEVEL"},
			},
		},
	}
	app.Commands = []*cli.Command{
		cmdRecordGet,
		cmdRecordList,
		cmdFirehose,
		cmdResolve,
		cmdRepo,
		cmdBlob,
		cmdLex,
		cmdAccount,
		cmdPLC,
		cmdBsky,
		cmdRecord,
		cmdSyntax,
		cmdKey,
		cmdPds,
		cmdXRPC,
		cmdRelay,
	}
	return app.Run(args)
}
