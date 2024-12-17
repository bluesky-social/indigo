package main

import (
	"os"
	"path/filepath"

	"github.com/bluesky-social/indigo/api"
	"github.com/bluesky-social/indigo/carstore"
	"github.com/bluesky-social/indigo/pds"
	"github.com/bluesky-social/indigo/plc"
	"github.com/bluesky-social/indigo/util/cliutil"

	_ "github.com/joho/godotenv/autoload"
	_ "go.uber.org/automaxprocs"

	"github.com/carlmjohnson/versioninfo"
	"github.com/urfave/cli/v2"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/exporters/jaeger"
	"go.opentelemetry.io/otel/sdk/resource"
	tracesdk "go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.4.0"
	"gorm.io/plugin/opentelemetry/tracing"
)

func main() {
	run(os.Args)
}

func run(args []string) {

	app := cli.App{
		Name:    "laputa",
		Usage:   "bluesky PDS in golang",
		Version: versioninfo.Short(),
	}

	app.Flags = []cli.Flag{
		&cli.BoolFlag{
			Name: "jaeger",
		},
		&cli.StringFlag{
			Name:    "db-url",
			Value:   "sqlite://./data/laputa/pds.sqlite",
			EnvVars: []string{"DATABASE_URL"},
		},
		&cli.StringFlag{
			Name:    "carstore-db-url",
			Value:   "sqlite://./data/laputa/carstore.sqlite",
			EnvVars: []string{"CARSTORE_DATABASE_URL"},
		},
		&cli.BoolFlag{
			Name: "db-tracing",
		},
		&cli.StringFlag{
			Name:  "name",
			Usage: "hostname of this PDS instance",
			Value: "localhost:4989",
		},
		&cli.StringFlag{
			Name:    "plc-host",
			Usage:   "method, hostname, and port of PLC registry",
			Value:   "https://plc.directory",
			EnvVars: []string{"ATP_PLC_HOST"},
		},
		&cli.StringFlag{
			Name:    "data-dir",
			Usage:   "path of directory for CAR files and other data",
			Value:   "data/laputa",
			EnvVars: []string{"DATA_DIR"},
		},
		&cli.StringFlag{
			Name:    "jwt-secret",
			Usage:   "secret used for authenticating JWT tokens",
			Value:   "jwtsecretplaceholder",
			EnvVars: []string{"ATP_JWT_SECRET"},
		},
		&cli.StringFlag{
			Name:    "handle-domains",
			Usage:   "comma-separated list of domain suffixes for handle registration",
			Value:   ".test",
			EnvVars: []string{"ATP_PDS_HANDLE_DOMAINS"},
		},
		&cli.IntFlag{
			Name:    "max-carstore-connections",
			EnvVars: []string{"MAX_CARSTORE_CONNECTIONS"},
			Value:   40,
		},
		&cli.IntFlag{
			Name:    "max-metadb-connections",
			EnvVars: []string{"MAX_METADB_CONNECTIONS"},
			Value:   40,
		},
	}

	app.Commands = []*cli.Command{
		generateKeyCmd,
	}

	app.Action = func(cctx *cli.Context) error {

		if cctx.Bool("jaeger") {
			url := "http://localhost:14268/api/traces"
			exp, err := jaeger.New(jaeger.WithCollectorEndpoint(jaeger.WithEndpoint(url)))
			if err != nil {
				return err
			}
			tp := tracesdk.NewTracerProvider(
				// Always be sure to batch in production.
				tracesdk.WithBatcher(exp),
				// Record information about this application in a Resource.
				tracesdk.WithResource(resource.NewWithAttributes(
					semconv.SchemaURL,
					semconv.ServiceNameKey.String("pds"),
					attribute.String("environment", "test"),
					attribute.Int64("ID", 1),
				)),
			)

			otel.SetTracerProvider(tp)
		}

		dbtracing := cctx.Bool("db-tracing")
		datadir := cctx.String("data-dir")
		csdir := filepath.Join(datadir, "carstore")
		keypath := filepath.Join(datadir, "server.key")
		jwtsecret := []byte(cctx.String("jwt-secret"))

		// TODO(bnewbold): split this on comma, and have PDS support multiple
		// domain suffixes that can be registered
		pdsdomain := cctx.String("handle-domains")

		// ensure data directories exist; won't error if it does
		os.MkdirAll(csdir, os.ModePerm)

		// default postgres setup: postgresql://postgres:password@localhost:5432/pdsdb?sslmode=disable
		db, err := cliutil.SetupDatabase(cctx.String("db-url"), cctx.Int("max-metadb-connections"))
		if err != nil {
			return err
		}

		// default postgres setup: postgresql://postgres:password@localhost:5432/cardb?sslmode=disable
		csdb, err := cliutil.SetupDatabase(cctx.String("carstore-db-url"), cctx.Int("max-carstore-connections"))
		if err != nil {
			return err
		}

		if dbtracing {
			if err := db.Use(tracing.NewPlugin()); err != nil {
				return err
			}
			if err := csdb.Use(tracing.NewPlugin()); err != nil {
				return err
			}
		}

		cstore, err := carstore.NewCarStore(csdb, []string{csdir})
		if err != nil {
			return err
		}

		var didr plc.PLCClient
		if plchost := cctx.String("plc-host"); plchost != "" {
			didr = &api.PLCServer{Host: plchost}
		} else {
			didr = plc.NewFakeDid(db)
		}

		pdshost := cctx.String("name")

		key, err := cliutil.LoadKeyFromFile(keypath)
		if err != nil {
			return err
		}

		srv, err := pds.NewServer(db, cstore, key, pdsdomain, pdshost, didr, jwtsecret)
		if err != nil {
			return err
		}

		return srv.RunAPI(":4989")
	}

	app.RunAndExitOnError()
}

var generateKeyCmd = &cli.Command{
	Name: "gen-key",
	Flags: []cli.Flag{
		&cli.StringFlag{
			Name:    "output",
			Aliases: []string{"o"},
			Value:   "data/laputa/server.key",
		},
	},
	Action: func(cctx *cli.Context) error {
		fname := cctx.String("output")
		return cliutil.GenerateKeyToFile(fname)
	},
}
