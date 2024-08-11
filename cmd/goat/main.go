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
		Usage:   "golang atproto CLI tool",
		Version: versioninfo.Short(),
	}
	app.Commands = []*cli.Command{
		cmdRecordGet,
		cmdRecordList,
		cmdResolve,
		cmdRepo,
		cmdBlob,
		cmdAccount,
		cmdPLC,
		cmdBsky,
		cmdRecord,
		cmdSyntax,
	}
	return app.Run(args)
}
