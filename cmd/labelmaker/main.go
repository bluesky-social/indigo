package main

import (
	"os"

	"github.com/bluesky-social/indigo/api"
	"github.com/bluesky-social/indigo/carstore"
	"github.com/bluesky-social/indigo/pds"
	"github.com/bluesky-social/indigo/plc"
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
			Name:  "labelmakerhost",
			Usage: "hostname of the labelmaker",
			Value: "localhost:2210",
		},
		&cli.StringFlag{
			Name:  "plc",
			Usage: "hostname of the plc",
		},
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
					semconv.ServiceNameKey.String("labelmaker"),
					attribute.String("environment", "test"),
					attribute.Int64("ID", 1),
				)),
			)

			otel.SetTracerProvider(tp)
		}

		pgdb := cctx.Bool("postgres")
		dbtracing := cctx.Bool("dbtracing")

		// ensure data directory exists; won't error if it does
		os.MkdirAll("data/labelmaker/", os.ModePerm)

		var pdsdial gorm.Dialector
		if pgdb {
			dsn := "host=localhost user=postgres password=password dbname=pdsdb port=5432 sslmode=disable"
			pdsdial = postgres.Open(dsn)
		} else {
			pdsdial = sqlite.Open("data/labelmaker/pds.sqlite")
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
			cardial = sqlite.Open("data/labelmaker/carstore.sqlite")
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

		cs, err := carstore.NewCarStore(carstdb, "data/labelmaker/carstore")
		if err != nil {
			return err
		}

		var didr plc.PLCClient
		if plchost := cctx.String("plc"); plchost != "" {
			didr = &api.PLCServer{Host: plchost}
		} else {
			didr = plc.NewFakeDid(db)
		}

		labelmakerhost := cctx.String("labelmakerhost")
		srv, err := pds.NewServer(db, cs, "data/labelmaker/labelmaker.key", ".labelmakertest", labelmakerhost, didr, []byte("jwtsecretplaceholder"))
		if err != nil {
			return err
		}

		return srv.RunAPI(":2210")
	}

	app.RunAndExitOnError()
}
