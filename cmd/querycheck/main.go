package main

import (
	"context"
	"fmt"
	"log"
	"log/slog"
	"os"
	"os/signal"
	"sync"
	"syscall"

	"net/http"
	_ "net/http/pprof"

	"github.com/bluesky-social/indigo/querycheck"
	"github.com/bluesky-social/indigo/util/tracing"
	"github.com/labstack/echo-contrib/pprof"
	"github.com/labstack/echo/v4"

	"github.com/labstack/echo/v4/middleware"

	"github.com/prometheus/client_golang/prometheus/promhttp"
	"go.opentelemetry.io/otel/trace"

	"github.com/carlmjohnson/versioninfo"
	"github.com/urfave/cli/v2"
)

func main() {
	app := cli.App{
		Name:    "querycheck",
		Usage:   "a postgresql query plan checker",
		Version: versioninfo.Short(),
	}

	app.Flags = []cli.Flag{
		&cli.StringFlag{
			Name:    "postgres-url",
			Usage:   "postgres url for storing events",
			Value:   "postgres://postgres:postgres@localhost:5432/postgres?sslmode=disable",
			EnvVars: []string{"POSTGRES_URL"},
		},
		&cli.IntFlag{
			Name:    "port",
			Usage:   "port to serve metrics on",
			Value:   8080,
			EnvVars: []string{"PORT"},
		},
		&cli.StringFlag{
			Name:    "auth-token",
			Usage:   "auth token for accessing the querycheck api",
			Value:   "",
			EnvVars: []string{"AUTH_TOKEN"},
		},
	}

	app.Action = Querycheck

	err := app.Run(os.Args)
	if err != nil {
		log.Fatal(err)
	}
}

var tracer trace.Tracer

// Querycheck is the main function for querycheck
func Querycheck(cctx *cli.Context) error {
	ctx := cctx.Context
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	// Trap SIGINT to trigger a shutdown.
	signals := make(chan os.Signal, 1)
	signal.Notify(signals, syscall.SIGINT, syscall.SIGTERM)

	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{
		Level: slog.LevelInfo,
	}))
	defer func() {
		logger.Info("main function teardown")
	}()

	logger = logger.With("source", "querycheck_main")
	logger.Info("starting querycheck")

	// Registers a tracer Provider globally if the exporter endpoint is set
	if os.Getenv("OTEL_EXPORTER_OTLP_ENDPOINT") != "" {
		logger.Info("initializing tracer")
		shutdown, err := tracing.InstallExportPipeline(ctx, "Querycheck", 1)
		if err != nil {
			log.Fatal(err)
		}
		defer func() {
			if err := shutdown(ctx); err != nil {
				log.Fatal(err)
			}
		}()
	}

	wg := sync.WaitGroup{}

	// HTTP Server setup and Middleware Plumbing
	e := echo.New()
	e.HideBanner = true
	e.HidePort = true
	pprof.Register(e)
	e.GET("/metrics", echo.WrapHandler(promhttp.Handler()))
	e.Use(middleware.LoggerWithConfig(middleware.DefaultLoggerConfig))

	// Start the query checker
	querychecker, err := querycheck.NewQuerychecker(ctx, cctx.String("postgres-url"))
	if err != nil {
		log.Fatalf("failed to create querychecker: %+v\n", err)
	}

	// 	getLikeCountQuery := `SELECT *
	// FROM like_counts
	// WHERE actor_did = 'did:plc:q6gjnaw2blty4crticxkmujt'
	// 	AND ns = 'app.bsky.feed.post'
	// 	AND rkey = '3k3jf5lgbsw24'
	// LIMIT 1;`

	// 	querychecker.AddQuery(ctx, "get_like_count", getLikeCountQuery, time.Second*20)

	err = querychecker.Start()
	if err != nil {
		log.Fatalf("failed to start querychecker: %+v\n", err)
	}

	e.Use(func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c echo.Context) error {
			if cctx.String("auth-token") != "" && c.Request().Header.Get("Authorization") != cctx.String("auth-token") {
				return c.String(http.StatusUnauthorized, "unauthorized")
			}
			return next(c)
		}
	})

	e.GET("/query", querychecker.HandleGetQuery)
	e.GET("/queries", querychecker.HandleGetQueries)
	e.POST("/query", querychecker.HandleAddQuery)
	e.PUT("/query", querychecker.HandleUpdateQuery)
	e.DELETE("/query", querychecker.HandleDeleteQuery)

	// Start the metrics server
	wg.Add(1)
	go func() {
		logger.Info("starting metrics serverd", "port", cctx.Int("port"))
		if err := e.Start(fmt.Sprintf(":%d", cctx.Int("port"))); err != nil {
			logger.Error("failed to start metrics server", "err", err)
		}
		wg.Done()
	}()

	select {
	case <-signals:
		cancel()
		fmt.Println("shutting down on signal")
	case <-ctx.Done():
		fmt.Println("shutting down on context done")
	}

	logger.Info("shutting down, waiting for workers to clean up")

	if err := e.Shutdown(ctx); err != nil {
		logger.Error("failed to shut down metrics server", "err", err)
		wg.Done()
	}

	querychecker.Stop()

	wg.Wait()
	logger.Info("shut down successfully")

	return nil
}
