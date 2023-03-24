package main

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/bluesky-social/indigo/api"
	"github.com/bluesky-social/indigo/carstore"
	cliutil "github.com/bluesky-social/indigo/cmd/gosky/util"
	"github.com/bluesky-social/indigo/internal/engine"
	"github.com/bluesky-social/indigo/pds"
	"github.com/bluesky-social/indigo/plc"

	_ "github.com/joho/godotenv/autoload"

	logging "github.com/ipfs/go-log"
	"github.com/lestrrat-go/jwx/v2/jwk"
	"github.com/urfave/cli/v2"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/exporters/jaeger"
	"go.opentelemetry.io/otel/sdk/resource"
	tracesdk "go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.4.0"
	"gorm.io/plugin/opentelemetry/tracing"
)

var log = logging.Logger("laputa")

func main() {
	run(os.Args)
}

func run(args []string) {

	app := cli.App{
		Name:    "laputa",
		Usage:   "bluesky PDS in golang",
		Version: engine.Version,
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
		db, err := cliutil.SetupDatabase(cctx.String("db-url"))
		if err != nil {
			return err
		}

		// default postgres setup: postgresql://postgres:password@localhost:5432/cardb?sslmode=disable
		csdb, err := cliutil.SetupDatabase(cctx.String("carstore-db-url"))
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

		cstore, err := carstore.NewCarStore(csdb, csdir)
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
		srv, err := pds.NewServer(db, cstore, keypath, pdsdomain, pdshost, didr, jwtsecret)
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
		raw, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
		if err != nil {
			return fmt.Errorf("failed to generate new ECDSA private key: %s", err)
		}

		key, err := jwk.FromRaw(raw)
		if err != nil {
			return fmt.Errorf("failed to create ECDSA key: %s", err)
		}

		if _, ok := key.(jwk.ECDSAPrivateKey); !ok {
			return fmt.Errorf("expected jwk.ECDSAPrivateKey, got %T", key)
		}

		key.Set(jwk.KeyIDKey, "mykey")

		buf, err := json.MarshalIndent(key, "", "  ")
		if err != nil {
			return fmt.Errorf("failed to marshal key into JSON: %w", err)
		}

		fname := cctx.String("output")
		// ensure data directory exists; won't error if it does
		os.MkdirAll(filepath.Dir(fname), os.ModePerm)

		return os.WriteFile(fname, buf, 0664)
	},
}
