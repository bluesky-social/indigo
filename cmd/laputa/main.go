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
	"github.com/bluesky-social/indigo/pds"
	"github.com/bluesky-social/indigo/plc"
	"github.com/lestrrat-go/jwx/v2/jwk"
	"github.com/urfave/cli/v2"
	"gorm.io/driver/postgres"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/exporters/jaeger"
	"go.opentelemetry.io/otel/sdk/resource"
	tracesdk "go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.4.0"
	"gorm.io/plugin/opentelemetry/tracing"
)

func main() {
	app := cli.NewApp()

	app.Flags = []cli.Flag{
		&cli.BoolFlag{
			Name: "jaeger",
		},
		&cli.BoolFlag{
			// Temp flag for testing, eventually will just pass db connection strings here
			Name: "postgres",
		},
		&cli.BoolFlag{
			Name: "dbtracing",
		},
		&cli.StringFlag{
			Name:  "pdshost",
			Usage: "hostname of the pds",
			Value: "localhost:4989",
		},
		&cli.StringFlag{
			Name:  "plc",
			Usage: "hostname of the plc",
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

		pgdb := cctx.Bool("postgres")
		dbtracing := cctx.Bool("dbtracing")

		// ensure data directory exists; won't error if it does
		os.MkdirAll("data/pds/", os.ModePerm)

		var pdsdial gorm.Dialector
		if pgdb {
			dsn := "host=localhost user=postgres password=password dbname=pdsdb port=5432 sslmode=disable"
			pdsdial = postgres.Open(dsn)
		} else {
			pdsdial = sqlite.Open("data/pds/pds.sqlite")
		}
		db, err := gorm.Open(pdsdial, &gorm.Config{})
		if err != nil {
			return err
		}

		if dbtracing {
			if err := db.Use(tracing.NewPlugin()); err != nil {
				return err
			}
		}

		var cardial gorm.Dialector
		if pgdb {
			dsn2 := "host=localhost user=postgres password=password dbname=cardb port=5432 sslmode=disable"
			cardial = postgres.Open(dsn2)
		} else {
			cardial = sqlite.Open("data/pds/carstore.sqlite")
		}
		carstdb, err := gorm.Open(cardial, &gorm.Config{})
		if err != nil {
			return err
		}

		if dbtracing {
			if err := carstdb.Use(tracing.NewPlugin()); err != nil {
				return err
			}
		}

		cs, err := carstore.NewCarStore(carstdb, "data/pds/carstore")
		if err != nil {
			return err
		}

		var didr plc.PLCClient
		if plchost := cctx.String("plc"); plchost != "" {
			didr = &api.PLCServer{Host: plchost}
		} else {
			didr = plc.NewFakeDid(db)
		}

		pdshost := cctx.String("pdshost")
		srv, err := pds.NewServer(db, cs, "data/pds/server.key", ".test", pdshost, didr, []byte("jwtsecretplaceholder"))
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
			Name:  "filename",
			Value: "data/pds/server.key",
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

		fname := cctx.String("filename")
		// ensure data directory exists; won't error if it does
		os.MkdirAll(filepath.Dir(fname), os.ModePerm)

		return os.WriteFile(fname, buf, 0664)
	},
}
