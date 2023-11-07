package main

import (
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"os"

	"github.com/bluesky-social/indigo/atproto/lexicon"

	"github.com/urfave/cli/v2"
)

func main() {
	app := cli.App{
		Name:  "lex-tool",
		Usage: "informal debugging CLI tool for atproto lexicons",
	}
	app.Commands = []*cli.Command{
		&cli.Command{
			Name:   "parse-schema",
			Usage:  "parse an individual lexicon schema file (JSON)",
			Action: runParseSchema,
		},
		&cli.Command{
			Name:   "load-directory",
			Usage:  "try recursively loading all the schemas from a directory",
			Action: runLoadDirectory,
		},
	}
	h := slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelDebug})
	slog.SetDefault(slog.New(h))
	app.RunAndExitOnError()
}

func runParseSchema(cctx *cli.Context) error {
	p := cctx.Args().First()
	if p == "" {
		return fmt.Errorf("need to provide path to a schema file as an argument")
	}

	f, err := os.Open(p)
	if err != nil {
		return err
	}
	defer func() { _ = f.Close() }()

	b, err := io.ReadAll(f)
	if err != nil {
		return err
	}

	var sf lexicon.SchemaFile
	if err := json.Unmarshal(b, &sf); err != nil {
		return err
	}
	out, err := json.MarshalIndent(sf, "", "  ")
	if err != nil {
		return err
	}
	fmt.Println(string(out))
	return nil
}

func runLoadDirectory(cctx *cli.Context) error {
	p := cctx.Args().First()
	if p == "" {
		return fmt.Errorf("need to provide directory path as an argument")
	}

	c := lexicon.NewCatalog()
	err := c.LoadDirectory(p)
	if err != nil {
		return err
	}

	fmt.Println("success!")
	return nil
}
