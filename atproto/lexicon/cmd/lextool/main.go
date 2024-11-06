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
		&cli.Command{
			Name:   "validate-record",
			Usage:  "fetch from network, validate against catalog",
			Action: runValidateRecord,
		},
		&cli.Command{
			Name:   "validate-firehose",
			Usage:  "subscribe to a firehose, validate every known record against catalog",
			Action: runValidateFirehose,
		},
		&cli.Command{
			Name:   "resolve",
			Usage:  "resolves an NSID to a lexicon schema",
			Action: runResolve,
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

	c := lexicon.NewBaseCatalog()
	err := c.LoadDirectory(p)
	if err != nil {
		return err
	}

	fmt.Println("success!")
	return nil
}

func runResolve(cctx *cli.Context) error {
	ref := cctx.Args().First()
	if ref == "" {
		return fmt.Errorf("need to provide NSID as an argument")
	}

	c := lexicon.NewResolvingCatalog()
	schema, err := c.Resolve(ref)
	if err != nil {
		return err
	}

	out, err := json.MarshalIndent(schema, "", "  ")
	if err != nil {
		return err
	}
	fmt.Println(string(out))
	return nil
}
