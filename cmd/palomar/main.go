package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"strings"
	"time"

	_ "github.com/joho/godotenv/autoload"

	"github.com/bluesky-social/indigo/atproto/identity"
	"github.com/bluesky-social/indigo/search"
	"github.com/bluesky-social/indigo/util/cliutil"

	"github.com/bluesky-social/indigo/util/version"
	logging "github.com/ipfs/go-log"
	es "github.com/opensearch-project/opensearch-go/v2"
	cli "github.com/urfave/cli/v2"
)

var log = logging.Logger("palomar")

func main() {
	if err := run(os.Args); err != nil {
		log.Fatal(err)
	}
}

func run(args []string) error {

	app := cli.App{
		Name:    "palomar",
		Usage:   "search indexing and query service (using ES or OS)",
		Version: version.Version,
	}

	app.Flags = []cli.Flag{
		&cli.StringFlag{
			Name:    "elastic-cert-file",
			Usage:   "certificate file path",
			EnvVars: []string{"ES_CERT_FILE", "ELASTIC_CERT_FILE"},
		},
		&cli.StringFlag{
			Name:    "elastic-username",
			Usage:   "elasticsearch username",
			Value:   "admin",
			EnvVars: []string{"ES_USERNAME", "ELASTIC_USERNAME"},
		},
		&cli.StringFlag{
			Name:    "elastic-password",
			Usage:   "elasticsearch password",
			Value:   "admin",
			EnvVars: []string{"ES_PASSWORD", "ELASTIC_PASSWORD"},
		},
		&cli.StringFlag{
			Name:    "elastic-hosts",
			Usage:   "elasticsearch hosts (schema/host/port)",
			Value:   "http://localhost:9200",
			EnvVars: []string{"ES_HOSTS", "ELASTIC_HOSTS", "OPENSEARCH_URL", "ELASTICSEARCH_URL"},
		},
		&cli.StringFlag{
			Name:    "es-post-index",
			Usage:   "ES index for 'post' documents",
			Value:   "palomar_post",
			EnvVars: []string{"ES_POST_INDEX"},
		},
		&cli.StringFlag{
			Name:    "es-profile-index",
			Usage:   "ES index for 'profile' documents",
			Value:   "palomar_profile",
			EnvVars: []string{"ES_PROFILE_INDEX"},
		},
		&cli.StringFlag{
			Name:    "atp-bgs-host",
			Usage:   "hostname and port of BGS to subscribe to",
			Value:   "wss://bsky.social",
			EnvVars: []string{"ATP_BGS_HOST"},
		},
		&cli.StringFlag{
			Name:    "atp-plc-host",
			Usage:   "method, hostname, and port of PLC registry",
			Value:   "https://plc.directory",
			EnvVars: []string{"ATP_PLC_HOST"},
		},
		&cli.IntFlag{
			Name:    "max-metadb-connections",
			EnvVars: []string{"MAX_METADB_CONNECTIONS"},
			Value:   40,
		},
	}

	app.Commands = []*cli.Command{
		runCmd,
		elasticCheckCmd,
		searchPostCmd,
		searchProfileCmd,
	}

	return app.Run(args)
}

var runCmd = &cli.Command{
	Name:  "run",
	Usage: "combined indexing+query server",
	Flags: []cli.Flag{
		&cli.StringFlag{
			Name:    "database-url",
			Value:   "sqlite://data/palomar/search.db",
			EnvVars: []string{"DATABASE_URL"},
		},
		&cli.BoolFlag{
			Name:    "readonly",
			EnvVars: []string{"READONLY"},
		},
		&cli.StringFlag{
			Name:    "bind",
			Usage:   "IP or address, and port, to listen on for HTTP APIs",
			Value:   ":3999",
			EnvVars: []string{"PALOMAR_BIND"},
		},
	},
	Action: func(cctx *cli.Context) error {
		db, err := cliutil.SetupDatabase(cctx.String("database-url"), cctx.Int("max-metadb-connections"))
		if err != nil {
			return err
		}

		escli, err := createEsClient(cctx)
		if err != nil {
			return fmt.Errorf("failed to get elasticsearch: %w", err)
		}

		// TODO: replace this with "bingo" resolver
		base := identity.BaseDirectory{
			PLCURL: cctx.String("atp-plc-host"),
			HTTPClient: http.Client{
				Timeout: time.Second * 15,
			},
			TryAuthoritativeDNS: false,
		}
		dir := identity.NewCacheDirectory(&base, 50000, time.Hour*24, time.Minute*2)

		srv, err := search.NewServer(
			db,
			escli,
			&dir,
			search.Config{
				BGSHost:      cctx.String("atp-bgs-host"),
				ProfileIndex: cctx.String("es-profile-index"),
				PostIndex:    cctx.String("es-post-index"),
			},
		)
		if err != nil {
			return err
		}

		go func() {
			srv.RunAPI(cctx.String("bind"))
		}()

		if cctx.Bool("readonly") {
			select {}
		} else {
			ctx := context.TODO()
			if err := srv.RunIndexer(ctx); err != nil {
				return fmt.Errorf("failed to run indexer: %w", err)
			}
		}

		return nil
	},
}

var elasticCheckCmd = &cli.Command{
	Name: "elastic-check",
	Flags: []cli.Flag{
		&cli.StringFlag{
			Name: "elastic-cert",
		},
	},
	Action: func(cctx *cli.Context) error {
		escli, err := createEsClient(cctx)
		if err != nil {
			return err
		}

		// NOTE: this extra info check is redundant; createEsClient() already made this call and logged results
		inf, err := escli.Info()
		if err != nil {
			return fmt.Errorf("failed to get info: %w", err)
		}

		fmt.Println(inf)
		return nil

	},
}

func printHits(resp *search.EsSearchResponse) {
	fmt.Printf("%d hits in %d\n", len(resp.Hits.Hits), resp.Took)
	for _, hit := range resp.Hits.Hits {
		b, _ := json.Marshal(hit.Source)
		fmt.Println(string(b))
	}
	return
}

var searchPostCmd = &cli.Command{
	Name:  "search-post",
	Usage: "run a simple query against posts index",
	Action: func(cctx *cli.Context) error {
		escli, err := createEsClient(cctx)
		if err != nil {
			return err
		}
		res, err := search.DoSearchPosts(
			context.Background(),
			escli,
			cctx.String("es-post-index"),
			strings.Join(cctx.Args().Slice(), " "),
			0,
			20,
		)
		if err != nil {
			return err
		}
		printHits(res)
		return nil
	},
}

var searchProfileCmd = &cli.Command{
	Name:  "search-profile",
	Usage: "run a simple query against posts index",
	Flags: []cli.Flag{
		&cli.BoolFlag{
			Name: "typeahead",
		},
	},
	Action: func(cctx *cli.Context) error {
		escli, err := createEsClient(cctx)
		if err != nil {
			return err
		}
		if cctx.Bool("typeahead") {
			res, err := search.DoSearchProfilesTypeahead(
				context.Background(),
				escli,
				cctx.String("es-profile-index"),
				strings.Join(cctx.Args().Slice(), " "),
			)
			if err != nil {
				return err
			}
			printHits(res)
		} else {
			res, err := search.DoSearchProfiles(
				context.Background(),
				escli,
				cctx.String("es-profile-index"),
				strings.Join(cctx.Args().Slice(), " "),
				0,
				20,
			)
			if err != nil {
				return err
			}
			printHits(res)
		}
		return nil
	},
}

func createEsClient(cctx *cli.Context) (*es.Client, error) {

	addrs := []string{}
	if hosts := cctx.String("elastic-hosts"); hosts != "" {
		addrs = strings.Split(hosts, ",")
	}

	certfi := cctx.String("elastic-cert-file")
	var cert []byte
	if certfi != "" {
		b, err := os.ReadFile(certfi)
		if err != nil {
			return nil, err
		}

		cert = b
	}

	cfg := es.Config{
		Addresses: addrs,
		Username:  cctx.String("elastic-username"),
		Password:  cctx.String("elastic-password"),

		CACert: cert,
	}

	escli, err := es.NewClient(cfg)
	if err != nil {
		return nil, fmt.Errorf("failed to set up client: %w", err)
	}

	info, err := escli.Info()
	if err != nil {
		return nil, fmt.Errorf("cannot get escli info: %w", err)
	}
	defer info.Body.Close()
	log.Debug(info)

	return escli, nil
}
