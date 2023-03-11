package main

import (
	"context"
	"os"
	"path/filepath"

	"github.com/bluesky-social/indigo/carstore"
	cliutil "github.com/bluesky-social/indigo/cmd/gosky/util"
	"github.com/bluesky-social/indigo/labeling"
	"github.com/urfave/cli/v2"

	logging "github.com/ipfs/go-log"
	"github.com/joho/godotenv"
	"gorm.io/plugin/opentelemetry/tracing"
)

var log = logging.Logger("labelmaker")

func main() {

	// only try dotenv if it exists
	if _, err := os.Stat(".env"); err == nil {
		err := godotenv.Load()
		if err != nil {
			log.Fatal("Error loading .env file")
		}
	}

	run(os.Args)
}

func run(args []string) {

	app := cli.App{
		Name:  "labelmaker",
		Usage: "atproto content labeling daemon",
	}

	app.Flags = []cli.Flag{
		&cli.StringFlag{
			Name:    "db-url",
			Usage:   "database connection string for labelmaker database",
			Value:   "sqlite://./data/labelmaker/labelmaker.sqlite",
			EnvVars: []string{"DATABASE_URL"},
		},
		&cli.StringFlag{
			Name:    "carstore-db-url",
			Usage:   "database connection string for carstore database",
			Value:   "sqlite://./data/labelmaker/carstore.sqlite",
			EnvVars: []string{"CARSTORE_DATABASE_URL"},
		},
		&cli.BoolFlag{
			Name: "db-tracing",
		},
		&cli.StringFlag{
			Name:    "data-dir",
			Usage:   "path of directory for CAR files and other data",
			Value:   "data/labelmaker",
			EnvVars: []string{"DATA_DIR"},
		},
		&cli.StringFlag{
			Name:    "bgs-host",
			Usage:   "hostname and port of BGS to subscribe to",
			Value:   "localhost:2470",
			EnvVars: []string{"ATP_BGS_HOST"},
		},
		&cli.StringFlag{
			Name:    "plc-host",
			Usage:   "method, hostname, and port of PLC registry",
			Value:   "https://plc.directory",
			EnvVars: []string{"ATP_PLC_HOST"},
		},
		&cli.BoolFlag{
			Name:  "subscribe-insecure-ws",
			Usage: "when connecting to BGS instance, use ws:// instead of wss://",
		},
		&cli.StringFlag{
			Name:    "repo-did",
			Usage:   "DID for labelmaker repo",
			Value:   "did:plc:FAKE",
			EnvVars: []string{"LABELMAKER_REPO_DID"},
		},
		&cli.StringFlag{
			Name:    "repo-handle",
			Usage:   "handle for labelmaker repo",
			Value:   "labelmaker.test",
			EnvVars: []string{"LABELMAKER_REPO_HANDLE"},
		},
		&cli.StringFlag{
			Name:    "bind",
			Usage:   "IP or address, and port, to listen on for HTTP and WebSocket APIs",
			Value:   ":2210",
			EnvVars: []string{"LABELMAKER_BIND"},
		},
		&cli.StringFlag{
			Name:    "keyword-file",
			Usage:   "keyword filter config, as JSON file",
			EnvVars: []string{"LABELMAKER_KEYWORD_FILE"},
		},
	}

	app.Action = func(cctx *cli.Context) error {

		// ensure data directory exists; won't error if it does
		datadir := cctx.String("data-dir")
		csdir := filepath.Join(datadir, "carstore")
		os.MkdirAll(datadir, os.ModePerm)
		repoKeyPath := filepath.Join(datadir, "labelmaker.key")

		dburl := cctx.String("db-url")
		db, err := cliutil.SetupDatabase(dburl)
		if err != nil {
			return err
		}

		csdburl := cctx.String("carstore-db-url")
		csdb, err := cliutil.SetupDatabase(csdburl)
		if err != nil {
			return err
		}

		if cctx.Bool("db-tracing") {
			if err := db.Use(tracing.NewPlugin()); err != nil {
				return err
			}
			if err := csdb.Use(tracing.NewPlugin()); err != nil {
				return err
			}
		}

		os.MkdirAll(filepath.Dir(csdir), os.ModePerm)
		cstore, err := carstore.NewCarStore(csdb, csdir)
		if err != nil {
			return err
		}

		kwlFile := cctx.String("keyword-file")
		var kwl []labeling.KeywordLabeler
		if kwlFile != "" {
			kwl, err = labeling.LoadKeywordFile(kwlFile)
			if err != nil {
				return err
			}
		} else {
			// trivial examples
			kwl = append(kwl, labeling.KeywordLabeler{Value: "meta", Keywords: []string{"bluesky", "atproto"}})
			kwl = append(kwl, labeling.KeywordLabeler{Value: "wordle", Keywords: []string{"wordle"}})
			kwl = append(kwl, labeling.KeywordLabeler{Value: "definite-article", Keywords: []string{"the"}})
		}

		bgsUrl := cctx.String("bgs-host")
		plcUrl := cctx.String("plc-host")
		useWss := !cctx.Bool("subscribe-insecure-ws")
		repoDid := cctx.String("repo-did")
		repoHandle := cctx.String("repo-handle")
		bind := cctx.String("bind")

		serkey, err := labeling.LoadKeyFromFile(repoKeyPath)
		if err != nil {
			return err
		}

		repoUser := labeling.RepoConfig{
			Handle:     repoHandle,
			Did:        repoDid,
			SigningKey: serkey,
			UserId:     1,
		}

		srv, err := labeling.NewServer(db, cstore, kwl, repoUser, plcUrl, useWss)
		if err != nil {
			return err
		}

		srv.SubscribeBGS(context.TODO(), bgsUrl, useWss)
		return srv.RunAPI(bind)
	}

	app.RunAndExitOnError()
}
