package main

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"log"
	"log/slog"
	"net/http"
	"os"
	"strings"
	"time"

	_ "github.com/joho/godotenv/autoload"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp"
	"go.opentelemetry.io/otel/sdk/resource"
	tracesdk "go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.4.0"
	"golang.org/x/time/rate"

	"github.com/bluesky-social/indigo/atproto/identity"
	"github.com/bluesky-social/indigo/search"
	"github.com/bluesky-social/indigo/util/cliutil"

	"github.com/carlmjohnson/versioninfo"
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
		Version: versioninfo.Short(),
	}

	app.Flags = []cli.Flag{
		&cli.StringFlag{
			Name:    "elastic-cert-file",
			Usage:   "certificate file path",
			EnvVars: []string{"ES_CERT_FILE", "ELASTIC_CERT_FILE"},
		},
		&cli.BoolFlag{
			Name:    "elastic-insecure-ssl",
			Usage:   "if true, disable SSL cert validation",
			EnvVars: []string{"ES_INSECURE_SSL"},
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
			Value:   "0penSearch-Pal0mar",
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
			Name:    "atp-relay-host",
			Usage:   "hostname and port of Relay to subscribe to",
			Value:   "wss://bsky.network",
			EnvVars: []string{"ATP_RELAY_HOST", "ATP_BGS_HOST"},
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
		&cli.StringFlag{
			Name:    "log-level",
			Usage:   "log level (debug, info, warn, error)",
			Value:   "info",
			EnvVars: []string{"GOLOG_LOG_LEVEL", "LOG_LEVEL"},
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
		&cli.StringFlag{
			Name:    "metrics-listen",
			Usage:   "IP or address, and port, to listen on for metrics APIs",
			Value:   ":3998",
			EnvVars: []string{"PALOMAR_METRICS_LISTEN"},
		},
		&cli.IntFlag{
			Name:    "relay-sync-rate-limit",
			Usage:   "max repo sync (checkout) requests per second to upstream (Relay)",
			Value:   8,
			EnvVars: []string{"PALOMAR_RELAY_SYNC_RATE_LIMIT", "PALOMAR_BGS_SYNC_RATE_LIMIT"},
		},
		&cli.IntFlag{
			Name:    "index-max-concurrency",
			Usage:   "max number of concurrent index requests (HTTP POST) to search index",
			Value:   20,
			EnvVars: []string{"PALOMAR_INDEX_MAX_CONCURRENCY"},
		},
		&cli.IntFlag{
			Name:    "indexing-rate-limit",
			Usage:   "max number of documents per second to index",
			Value:   50_000,
			EnvVars: []string{"PALOMAR_INDEXING_RATE_LIMIT"},
		},
		&cli.IntFlag{
			Name:    "plc-rate-limit",
			Usage:   "max number of requests per second to PLC registry",
			Value:   100,
			EnvVars: []string{"PALOMAR_PLC_RATE_LIMIT"},
		},
		&cli.BoolFlag{
			Name:    "discover-repos",
			Usage:   "if true, discover repositories from the Relay",
			EnvVars: []string{"PALOMAR_DISCOVER_REPOS"},
			Value:   false,
		},
		&cli.StringFlag{
			Name:    "pagerank-file",
			EnvVars: []string{"PAGERANK_FILE"},
		},
		&cli.StringFlag{
			Name:    "bulk-posts-file",
			EnvVars: []string{"BULK_POSTS_FILE"},
		},
		&cli.StringFlag{
			Name:    "bulk-profiles-file",
			EnvVars: []string{"BULK_PROFILES_FILE"},
		},
	},
	Action: func(cctx *cli.Context) error {
		logLevel := slog.LevelInfo
		switch cctx.String("log-level") {
		case "debug":
			logLevel = slog.LevelDebug
		case "info":
			logLevel = slog.LevelInfo
		case "warn":
			logLevel = slog.LevelWarn
		case "error":
			logLevel = slog.LevelError
		}

		logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{
			Level:     logLevel,
			AddSource: true,
		}))
		slog.SetDefault(logger)

		readonly := cctx.Bool("readonly")

		// Enable OTLP HTTP exporter
		// For relevant environment variables:
		// https://pkg.go.dev/go.opentelemetry.io/otel/exporters/otlp/otlptrace#readme-environment-variables
		// At a minimum, you need to set
		// OTEL_EXPORTER_OTLP_ENDPOINT=http://localhost:4318
		if ep := os.Getenv("OTEL_EXPORTER_OTLP_ENDPOINT"); ep != "" {
			slog.Info("setting up trace exporter", "endpoint", ep)
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()

			exp, err := otlptracehttp.New(ctx)
			if err != nil {
				log.Fatal("failed to create trace exporter", "error", err)
			}
			defer func() {
				ctx, cancel := context.WithTimeout(context.Background(), time.Second)
				defer cancel()
				if err := exp.Shutdown(ctx); err != nil {
					slog.Error("failed to shutdown trace exporter", "error", err)
				}
			}()

			tp := tracesdk.NewTracerProvider(
				tracesdk.WithBatcher(exp),
				tracesdk.WithResource(resource.NewWithAttributes(
					semconv.SchemaURL,
					semconv.ServiceNameKey.String("palomar"),
					attribute.String("env", os.Getenv("ENVIRONMENT")),         // DataDog
					attribute.String("environment", os.Getenv("ENVIRONMENT")), // Others
					attribute.Int64("ID", 1),
				)),
			)
			otel.SetTracerProvider(tp)
		}

		escli, err := createEsClient(cctx)
		if err != nil {
			return fmt.Errorf("failed to get elasticsearch: %w", err)
		}

		base := identity.BaseDirectory{
			PLCURL: cctx.String("atp-plc-host"),
			HTTPClient: http.Client{
				Timeout: time.Second * 15,
			},
			PLCLimiter:            rate.NewLimiter(rate.Limit(cctx.Int("plc-rate-limit")), 1),
			TryAuthoritativeDNS:   true,
			SkipDNSDomainSuffixes: []string{".bsky.social"},
		}
		dir := identity.NewCacheDirectory(&base, 1_500_000, time.Hour*24, time.Minute*2, time.Minute*5)

		apiConfig := search.ServerConfig{
			Logger:       logger,
			ProfileIndex: cctx.String("es-profile-index"),
			PostIndex:    cctx.String("es-post-index"),
		}

		srv, err := search.NewServer(escli, &dir, apiConfig)
		if err != nil {
			return err
		}

		// Configure the indexer if we're not in readonly mode
		if !readonly {
			db, err := cliutil.SetupDatabase(cctx.String("database-url"), cctx.Int("max-metadb-connections"))
			if err != nil {
				return fmt.Errorf("failed to set up database: %w", err)
			}

			indexerConfig := search.IndexerConfig{
				RelayHost:           cctx.String("atp-relay-host"),
				ProfileIndex:        cctx.String("es-profile-index"),
				PostIndex:           cctx.String("es-post-index"),
				Logger:              logger,
				RelaySyncRateLimit:  cctx.Int("relay-sync-rate-limit"),
				IndexMaxConcurrency: cctx.Int("index-max-concurrency"),
				DiscoverRepos:       cctx.Bool("discover-repos"),
				IndexingRateLimit:   cctx.Int("indexing-rate-limit"),
			}

			idx, err := search.NewIndexer(db, escli, &dir, indexerConfig)
			if err != nil {
				return fmt.Errorf("failed to set up indexer: %w", err)
			}

			srv.Indexer = idx
		}

		go func() {
			if err := srv.RunMetrics(cctx.String("metrics-listen")); err != nil {
				slog.Error("failed to start metrics endpoint", "error", err)
				panic(fmt.Errorf("failed to start metrics endpoint: %w", err))
			}
		}()

		go func() {
			srv.RunAPI(cctx.String("bind"))
		}()

		// If we're in readonly mode, just block forever
		if readonly {
			select {}
		} else if cctx.String("pagerank-file") != "" && srv.Indexer != nil {
			// If we're not in readonly mode, and we have a pagerank file, update pageranks
			ctx := context.Background()
			if err := srv.Indexer.BulkIndexPageranks(ctx, cctx.String("pagerank-file")); err != nil {
				return fmt.Errorf("failed to update pageranks: %w", err)
			}
		} else if cctx.String("bulk-posts-file") != "" && srv.Indexer != nil {
			// If we're not in readonly mode, and we have a bulk posts file, index posts
			ctx := context.Background()
			if err := srv.Indexer.BulkIndexPosts(ctx, cctx.String("bulk-posts-file")); err != nil {
				return fmt.Errorf("failed to bulk index posts: %w", err)
			}
		} else if cctx.String("bulk-profiles-file") != "" && srv.Indexer != nil {
			// If we're not in readonly mode, and we have a bulk profiles file, index profiles
			ctx := context.Background()
			if err := srv.Indexer.BulkIndexProfiles(ctx, cctx.String("bulk-profiles-file")); err != nil {
				return fmt.Errorf("failed to bulk index profiles: %w", err)
			}
		} else if srv.Indexer != nil {
			// Otherwise, just run the indexer
			ctx := context.Background()
			if err := srv.Indexer.EnsureIndices(ctx); err != nil {
				return fmt.Errorf("failed to create opensearch indices: %w", err)
			}
			if err := srv.Indexer.RunIndexer(ctx); err != nil {
				return fmt.Errorf("failed to run indexer: %w", err)
			}
		}

		return nil
	},
}

var elasticCheckCmd = &cli.Command{
	Name:  "elastic-check",
	Flags: []cli.Flag{},
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
			&search.PostSearchParams{
				Query:  strings.Join(cctx.Args().Slice(), " "),
				Offset: 0,
				Size:   20,
			},
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
				&search.ActorSearchParams{
					Query: strings.Join(cctx.Args().Slice(), " "),
					Size:  10,
				},
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
				&search.ActorSearchParams{
					Query:  strings.Join(cctx.Args().Slice(), " "),
					Offset: 0,
					Size:   20,
				},
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

	insecure := cctx.Bool("elastic-insecure-ssl")

	cfg := es.Config{
		Addresses: addrs,
		Username:  cctx.String("elastic-username"),
		Password:  cctx.String("elastic-password"),
		CACert:    cert,
		Transport: &http.Transport{
			MaxIdleConnsPerHost: 20,
			TLSClientConfig: &tls.Config{
				InsecureSkipVerify: insecure,
			},
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
