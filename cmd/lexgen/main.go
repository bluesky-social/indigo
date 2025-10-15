package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"go/format"
	"io/fs"
	"log/slog"
	"os"
	"path"
	"path/filepath"

	_ "github.com/joho/godotenv/autoload"

	"github.com/bluesky-social/indigo/atproto/lexicon"

	"github.com/earthboundkid/versioninfo/v2"
	"github.com/urfave/cli/v3"
	"golang.org/x/tools/imports"
)

func main() {
	if err := run(os.Args); err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(-1)
	}
}

func run(args []string) error {

	app := cli.Command{
		Name:  "lexgen",
		Usage: "AT lexicon code generation for Go",
		//Description: "",
		Version: versioninfo.Short(),
	}
	app.Commands = []*cli.Command{
		cmdGenerate,
		cmdDev,
	}
	return app.Run(context.Background(), args)
}

var cmdGenerate = &cli.Command{
	Name:      "generate",
	Usage:     "check schema syntax, best practices, and style",
	ArgsUsage: `<file-or-dir>*`,
	Flags: []cli.Flag{
		&cli.StringFlag{
			Name:    "lexicons-dir",
			Value:   "./lexicons/",
			Usage:   "base directory for project Lexicon files",
			Sources: cli.EnvVars("LEXICONS_DIR"),
		},
		&cli.StringFlag{
			Name:    "output-dir",
			Value:   "./lexgen-out/",
			Usage:   "base directory for output packages",
			Sources: cli.EnvVars("OUTPUT_DIR"),
		},
	},
	Action: runGenerate,
}

func runGenerate(ctx context.Context, cmd *cli.Command) error {
	paths := cmd.Args().Slice()
	if !cmd.Args().Present() {
		paths = []string{cmd.String("lexicons-dir")}
		_, err := os.Stat(paths[0])
		if err != nil {
			return fmt.Errorf("no path arguments specified and default lexicon directory not found\n%w", err)
		}
	}

	// load all directories
	cat := lexicon.NewBaseCatalog()
	lexDir := cmd.String("lexicons-dir")
	ldinfo, err := os.Stat(lexDir)
	if err == nil && ldinfo.IsDir() {
		if err := cat.LoadDirectory(lexDir); err != nil {
			return err
		}
	}

	slog.Debug("starting lint run")
	for _, p := range paths {
		finfo, err := os.Stat(p)
		if err != nil {
			return fmt.Errorf("failed loading %s: %w", p, err)
		}
		if finfo.IsDir() {
			if p != cmd.String("lexicons-dir") {
				// HACK: load first directory
				if err := cat.LoadDirectory(p); err != nil {
					return err
				}
			}
			if err := filepath.WalkDir(p, func(fp string, d fs.DirEntry, err error) error {
				if d.IsDir() || path.Ext(fp) != ".json" {
					return nil
				}
				return genFile(ctx, cmd, &cat, fp)
			}); err != nil {
				return err
			}
			continue
		}
		if err := genFile(ctx, cmd, &cat, p); err != nil {
			return err
		}
	}
	return nil
}

func genFile(ctx context.Context, cmd *cli.Command, cat lexicon.Catalog, p string) error {
	b, err := os.ReadFile(p)
	if err != nil {
		return fmt.Errorf("failed to read lexicon schema from disk (%s): %w", p, err)
	}

	// parse file regularly
	// TODO: use json/v2 when available for case-sensitivity
	var sf lexicon.SchemaFile

	// two-part parsing before looking at errors
	err = json.Unmarshal(b, &sf)
	if err == nil {
		err = sf.FinishParse()
	}
	if err != nil {
		return fmt.Errorf("failed to parse lexicon schema from disk (%s): %w", p, err)
	}

	flat, err := FlattenSchemaFile(&sf)
	if err != nil {
		return fmt.Errorf("internal codegen flattening error (%s): %w", p, err)
	}

	buf := new(bytes.Buffer)

	gen := FlatGenerator{
		Config: LegacyConfig(),
		Lex:    flat,
		Cat:    cat,
		Out:    buf,
	}
	if err := gen.WriteLexicon(); err != nil {
		return fmt.Errorf("failed to format codegen output (%s): %w", p, err)
	}

	outPath := path.Join(cmd.String("output-dir"), gen.pkgName(), gen.fileName())
	if err := os.MkdirAll(path.Dir(outPath), 0755); err != nil {
		return err
	}

	fixImports := false
	if fixImports {
		fmtOpts := imports.Options{
			Comments:  true,
			TabIndent: false,
			TabWidth:  4,
		}
		formatted, err := imports.Process(outPath, buf.Bytes(), &fmtOpts)
		if err != nil {
			return fmt.Errorf("failed to format codegen output (%s): %w", p, err)
		}
		return os.WriteFile(outPath, formatted, 0644)
	} else {
		formatted, err := format.Source(buf.Bytes())
		if err != nil {
			return fmt.Errorf("failed to format codegen output (%s): %w", p, err)
		}
		return os.WriteFile(outPath, formatted, 0644)
	}
}

var cmdDev = &cli.Command{
	Name:      "dev",
	ArgsUsage: `<file>`,
	Flags: []cli.Flag{
		&cli.StringFlag{
			Name:    "lexicons-dir",
			Value:   "./lexicons/",
			Usage:   "base directory for project Lexicon files",
			Sources: cli.EnvVars("LEXICONS_DIR"),
		},
	},
	Action: runDev,
}

func runDev(ctx context.Context, cmd *cli.Command) error {
	if !cmd.Args().Present() {
		return fmt.Errorf("require one or more paths")
	}

	b, err := os.ReadFile(cmd.Args().First())
	if err != nil {
		return err
	}

	var sf lexicon.SchemaFile
	err = json.Unmarshal(b, &sf)
	if err == nil {
		err = sf.FinishParse()
	}
	if err != nil {
		return err
	}

	flat, err := FlattenSchemaFile(&sf)
	if err != nil {
		return err
	}

	cat := lexicon.NewBaseCatalog()
	gen := FlatGenerator{
		Config: &GenConfig{
			RegisterLexiconTypeID: true,
		},
		Cat: &cat,
		Lex: flat,
		Out: os.Stdout,
	}
	return gen.WriteLexicon()
}
