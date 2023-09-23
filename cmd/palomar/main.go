package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"strings"
	"time"

	_ "github.com/joho/godotenv/autoload"
	"golang.org/x/time/rate"

	"github.com/bluesky-social/indigo/atproto/identity"
	"github.com/bluesky-social/indigo/search"
	"github.com/bluesky-social/indigo/util/cliutil"

	"github.com/bluesky-social/indigo/util/version"
	es "github.com/opensearch-project/opensearch-go/v2"
	cli "github.com/urfave/cli/v2"
)

func main() {
	if err := run(os.Args); err != nil {
		slog.Error("exiting", "err", err)
		os.Exit(-1)
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
			EnvVars: []string{"PALOMAR_READONLY", "READONLY"},
		},
		&cli.StringFlag{
			Name:    "bind",
			Usage:   "IP or address, and port, to listen on for HTTP APIs",
			Value:   ":3999",
			EnvVars: []string{"PALOMAR_BIND"},
		},
		&cli.IntFlag{
			Name:    "bgs-sync-rate-limit",
			Usage:   "max repo sync (checkout) requests per second to upstream (BGS)",
			Value:   8,
			EnvVars: []string{"PALOMAR_BGS_SYNC_RATE_LIMIT"},
		},
		&cli.IntFlag{
			Name:    "index-max-concurrency",
			Usage:   "max number of concurrent index requests (HTTP POST) to search index",
			Value:   20,
			EnvVars: []string{"PALOMAR_INDEX_MAX_CONCURRENCY"},
		},
		&cli.IntFlag{
			Name:    "plc-rate-limit",
			Usage:   "max number of requests per second to PLC registry",
			Value:   100,
			EnvVars: []string{"PALOMAR_PLC_RATE_LIMIT"},
		},
	},
	Action: func(cctx *cli.Context) error {
		logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{
			Level: slog.LevelInfo,
		}))
		slog.SetDefault(logger)

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
			PLCLimiter:            rate.NewLimiter(rate.Limit(cctx.Int("plc-rate-limit")), 1),
			TryAuthoritativeDNS:   true,
			SkipDNSDomainSuffixes: []string{".bsky.social"},
		}
		dir := identity.NewCacheDirectory(&base, 200000, time.Hour*24, time.Minute*2)

		srv, err := search.NewServer(
			db,
			escli,
			&dir,
			search.Config{
				BGSHost:             cctx.String("atp-bgs-host"),
				ProfileIndex:        cctx.String("es-profile-index"),
				PostIndex:           cctx.String("es-post-index"),
				Logger:              logger,
				BGSSyncRateLimit:    cctx.Int("bgs-sync-rate-limit"),
				IndexMaxConcurrency: cctx.Int("index-max-concurrency"),
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
			ctx := context.Background()
			if err := srv.EnsureIndices(ctx); err != nil {
				return fmt.Errorf("failed to create opensearch indices: %w", err)
			}
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
		defer inf.Body.Close()
		if inf.IsError() {
			return fmt.Errorf("failed to get info")
		}
		slog.Info("opensearch client connected", "client_info", inf)

		resp, err := escli.Indices.Exists([]string{cctx.String("es-profile-index"), cctx.String("es-post-index")})
		if err != nil {
			return fmt.Errorf("failed to check index existence: %w", err)
		}
		defer resp.Body.Close()
		if inf.IsError() {
			return fmt.Errorf("failed to check index existence")
		}
		slog.Info("index existence", "resp", resp)

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
			identity.DefaultDirectory(), // TODO: parse PLC arg
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
				10,
			)
			if err != nil {
				return err
			}
			printHits(res)
		} else {
			res, err := search.DoSearchProfiles(
				context.Background(),
				identity.DefaultDirectory(), // TODO: parse PLC arg
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
		CACert:    cert,
		Transport: &http.Transport{
			MaxIdleConnsPerHost: 20,
		},
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
	slog.Debug("opensearch client initialized", "info", info)

	return escli, nil
}
