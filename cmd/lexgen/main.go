package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"go/format"
	"io/fs"
	"os"
	"path"
	"path/filepath"

	_ "github.com/joho/godotenv/autoload"

	"github.com/bluesky-social/indigo/atproto/lexicon"
	"github.com/bluesky-social/indigo/lex/lexgen"

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
		cmdLegacy,
		cmdGen,
	}
	return app.Run(context.Background(), args)
}

var cmdLegacy = &cli.Command{
	Name:      "legacy",
	Usage:     "generate code with legacy behaviors (for indigo only)",
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
			Value:   "./lexgen-output/",
			Usage:   "base directory for output packages",
			Sources: cli.EnvVars("OUTPUT_DIR"),
		},
		&cli.BoolFlag{
			Name:  "legacy-mode",
			Value: true,
		},
	},
	Action: runGen,
}

var cmdGen = &cli.Command{
	Name:      "gen",
	Usage:     "generate code for lexicons",
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
			Value:   "./lexgen-output/",
			Usage:   "base directory for output packages",
			Sources: cli.EnvVars("OUTPUT_DIR"),
		},
		&cli.BoolFlag{
			Name:  "no-imports-tidy",
			Usage: "skip cleanup of go imports in writen output",
		},
	},
	Action: runGen,
}

func collectPaths(cmd *cli.Command) ([]string, lexicon.Catalog, error) {
	paths := cmd.Args().Slice()
	if !cmd.Args().Present() {
		paths = []string{cmd.String("lexicons-dir")}
		_, err := os.Stat(paths[0])
		if err != nil {
			return nil, nil, fmt.Errorf("no path arguments specified and default lexicon directory not found\n%w", err)
		}
	}

	// load all directories
	cat := lexicon.NewBaseCatalog()
	lexDir := cmd.String("lexicons-dir")
	ldinfo, err := os.Stat(lexDir)
	if err == nil && ldinfo.IsDir() {
		if err := cat.LoadDirectory(lexDir); err != nil {
			return nil, nil, err
		}
	}

	filePaths := []string{}

	for _, p := range paths {
		finfo, err := os.Stat(p)
		if err != nil {
			return nil, nil, fmt.Errorf("failed loading %s: %w", p, err)
		}
		if finfo.IsDir() {
			if p != cmd.String("lexicons-dir") {
				// HACK: load first directory
				if err := cat.LoadDirectory(p); err != nil {
					return nil, nil, err
				}
			}
			if err := filepath.WalkDir(p, func(fp string, d fs.DirEntry, err error) error {
				if d.IsDir() || path.Ext(fp) != ".json" {
					return nil
				}
				filePaths = append(filePaths, fp)
				return nil
			}); err != nil {
				return nil, nil, err
			}
			continue
		}
		filePaths = append(filePaths, p)
	}
	return filePaths, &cat, nil
}

func runGen(ctx context.Context, cmd *cli.Command) error {

	filePaths, cat, err := collectPaths(cmd)
	if err != nil {
		return err
	}

	for _, p := range filePaths {
		if err := genFile(ctx, cmd, cat, p); err != nil {
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
	// NOTE: use json/v2 when it stabilizes for case-sensitivity
	var sf lexicon.SchemaFile

	err = json.Unmarshal(b, &sf)
	if err == nil {
		err = sf.FinishParse()
	}
	if err != nil {
		return fmt.Errorf("failed to parse lexicon schema from disk (%s): %w", p, err)
	}

	flat, err := lexgen.FlattenSchemaFile(&sf)
	if err != nil {
		return fmt.Errorf("internal codegen flattening error (%s): %w", p, err)
	}

	cfg := lexgen.NewGenConfig()
	if cmd.Bool("legacy-mode") {
		cfg = lexgen.LegacyConfig()
	}

	buf := new(bytes.Buffer)
	gen := lexgen.FlatGenerator{
		Config: cfg,
		Lex:    flat,
		Cat:    cat,
		Out:    buf,
	}
	if err := gen.WriteLexicon(); err != nil {
		return fmt.Errorf("failed to format codegen output (%s): %w", p, err)
	}

	outPath := path.Join(cmd.String("output-dir"), gen.PkgName(), gen.FileName())
	if err := os.MkdirAll(path.Dir(outPath), 0755); err != nil {
		return err
	}

	if !cmd.Bool("no-imports-tidy") {
		// NOTE: processing imports per file gets slow if imports are missing
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
