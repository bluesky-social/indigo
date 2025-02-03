package main

import (
	"fmt"
	"log/slog"
	"os"

	_ "github.com/joho/godotenv/autoload"

	"github.com/bluesky-social/indigo/atproto/syntax"

	"github.com/carlmjohnson/versioninfo"
	"github.com/urfave/cli/v2"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

var (
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
		Name:  "lexidex",
		Usage: "atproto Lexicon index and schema browser",
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
					Value:    ":8500",
					EnvVars:  []string{"LEXIDEX_BIND"},
				},
				&cli.BoolFlag{
					Name:     "debug",
					Usage:    "Enable debug mode",
					Value:    false,
					Required: false,
					EnvVars:  []string{"DEBUG"},
				},
				&cli.StringFlag{
					Name:    "sqlite-path",
					Usage:   "Database file path",
					Value:   "./lexidex.sqlite",
					EnvVars: []string{"LEXIDEX_SQLITE_PATH"},
				},
			},
		},
		&cli.Command{
			Name:   "crawl",
			Usage:  "crawl a single NSID",
			Action: runCrawl,
			Flags: []cli.Flag{
				&cli.StringFlag{
					Name:    "sqlite-path",
					Usage:   "Database file path",
					Value:   "./lexidex.sqlite",
					EnvVars: []string{"LEXIDEX_SQLITE_PATH"},
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

func runCrawl(cctx *cli.Context) error {
	ctx := cctx.Context

	s := cctx.Args().First()
	if s == "" {
		return fmt.Errorf("need to provide Lexicon NSID as an argument")
	}
	nsid, err := syntax.ParseNSID(s)
	if err != nil {
		return err
	}

	db, err := gorm.Open(sqlite.Open(cctx.String("sqlite-path")))
	if err != nil {
		return fmt.Errorf("failed to open db: %w", err)
	}

	RunAllMigrations(db)
	return CrawlLexicon(ctx, db, nsid)
}
