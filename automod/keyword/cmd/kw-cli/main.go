package main

import (
	"bufio"
	"context"
	"fmt"
	"log/slog"
	"os"

	"github.com/bluesky-social/indigo/automod/keyword"
	"github.com/bluesky-social/indigo/automod/setstore"

	"github.com/urfave/cli/v2"
)

func main() {
	app := cli.App{
		Name:  "kw-cli",
		Usage: "informal debugging CLI tool for keyword matching",
	}
	app.Commands = []*cli.Command{
		&cli.Command{
			Name:   "fuzzy",
			Usage:  "reads lines of text from stdin, runs regex fuzzy matching, outputs matches",
			Action: runFuzzy,
		},
		&cli.Command{
			Name:   "tokens",
			Usage:  "reads lines of text from stdin, tokenizes and matches against set",
			Action: runTokens,
			Flags: []cli.Flag{
				&cli.StringFlag{
					Name:  "json-set-file",
					Usage: "path to JSON file containing bad word sets",
					Value: "automod/rules/example_sets.json",
				},
				&cli.StringFlag{
					Name:  "set-name",
					Usage: "which set within the set file to use",
					Value: "bad-words",
				},
				&cli.BoolFlag{
					Name:  "identifiers",
					Usage: "whether to parse the line as identifiers (instead of text)",
				},
			},
		},
	}
	h := slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelDebug})
	slog.SetDefault(slog.New(h))
	app.RunAndExitOnError()
}

func runFuzzy(cctx *cli.Context) error {
	scanner := bufio.NewScanner(os.Stdin)
	for scanner.Scan() {
		line := scanner.Text()
		word := keyword.SlugContainsExplicitSlur(keyword.Slugify(line))
		if word != "" {
			fmt.Printf("MATCH\t%s\t%s\n", word, line)
		}
	}
	return nil
}

func runTokens(cctx *cli.Context) error {
	ctx := context.Background()
	sets := setstore.NewMemSetStore()
	if err := sets.LoadFromFileJSON(cctx.String("json-set-file")); err != nil {
		return err
	}
	setName := cctx.String("set-name")
	identMode := cctx.Bool("identifiers")
	scanner := bufio.NewScanner(os.Stdin)
	for scanner.Scan() {
		line := scanner.Text()
		var tokens []string
		if identMode {
			tokens = keyword.TokenizeIdentifier(line)
		} else {
			tokens = keyword.TokenizeText(line)
		}
		for _, tok := range tokens {
			match, err := sets.InSet(ctx, setName, tok)
			if err != nil {
				return err
			}
			if match {
				fmt.Printf("MATCH\t%s\t%s\n", tok, line)
			}
		}
	}
	return nil
}
