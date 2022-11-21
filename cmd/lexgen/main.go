package main

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	cli "github.com/urfave/cli/v2"
	lex "github.com/whyrusleeping/gosky/lex"
)

func findSchemas(dir string) ([]string, error) {
	var out []string
	err := filepath.Walk(dir, func(path string, info fs.FileInfo, err error) error {
		if err != nil {
			return err
		}

		if info.IsDir() {
			return nil
		}

		if strings.HasSuffix(path, ".json") {
			out = append(out, path)
		}

		return nil
	})
	if err != nil {
		return nil, err
	}

	return out, nil

}

func expandArgs(args []string) ([]string, error) {
	var out []string
	for _, a := range args {
		st, err := os.Stat(a)
		if err != nil {
			return nil, err
		}
		if st.IsDir() {
			s, err := findSchemas(a)
			if err != nil {
				return nil, err
			}
			out = append(out, s...)
		} else if strings.HasSuffix(a, ".json") {
			out = append(out, a)
		}
	}

	return out, nil
}

func main() {
	app := cli.NewApp()

	app.Flags = []cli.Flag{
		&cli.StringFlag{
			Name: "outdir",
		},
		&cli.StringFlag{
			Name: "prefix",
		},
	}
	app.Action = func(cctx *cli.Context) error {
		outdir := cctx.String("outdir")
		if outdir == "" {
			return fmt.Errorf("must specify output directory (--outdir)")
		}

		prefix := cctx.String("prefix")

		paths, err := expandArgs(cctx.Args().Slice())
		if err != nil {
			return err
		}

		var schemas []*lex.Schema
		for _, arg := range paths {
			s, err := lex.ReadSchema(arg)
			if err != nil {
				return fmt.Errorf("failed to read file %q: %w", arg, err)
			}

			schemas = append(schemas, s)
		}

		for i, s := range schemas {
			fname := filepath.Join(outdir, s.Name()+".go")

			if err := lex.GenCodeForSchema("schemagen", prefix, fname, true, s); err != nil {
				return fmt.Errorf("failed to process schema %q: %w", paths[i], err)
			}
		}

		return nil
	}

	app.RunAndExitOnError()
}
