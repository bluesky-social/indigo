package main

import (
	"bufio"
	"fmt"
	"log"
	"log/slog"
	"net/http"
	_ "net/http/pprof"
	"os"
	"strings"
	"time"

	"github.com/bluesky-social/indigo/graphd"
	"github.com/bluesky-social/indigo/graphd/handlers"
	"github.com/bluesky-social/indigo/util/version"
	"github.com/labstack/echo-contrib/echoprometheus"
	"github.com/labstack/echo/v4"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/urfave/cli/v2"
)

func main() {
	app := cli.App{
		Name:    "graphd",
		Usage:   "relational graph daemon",
		Version: version.Version,
	}

	app.Flags = []cli.Flag{
		&cli.BoolFlag{
			Name:  "debug",
			Usage: "enable debug logging",
		},
		&cli.IntFlag{
			Name:  "port",
			Usage: "listen port for http server",
			Value: 1323,
		},
		&cli.StringFlag{
			Name:  "graph-csv",
			Usage: "path to graph csv file",
		},
	}

	app.Action = GraphD

	err := app.Run(os.Args)
	if err != nil {
		log.Fatal(err)
	}

}

func GraphD(cctx *cli.Context) error {
	logLevel := slog.LevelInfo
	if cctx.Bool("debug") {
		logLevel = slog.LevelDebug
	}

	slog.SetDefault(slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{
		Level: logLevel,
	})))

	graph := graphd.NewGraph()

	start := time.Now()
	totalFollows := 0

	// Load all the follows from the graph CSV
	if cctx.String("graph-csv") != "" {
		f, err := os.Open(cctx.String("graph-csv"))
		if err != nil {
			return fmt.Errorf("failed to open graph CSV (%s): %w", cctx.String("graph-csv"), err)
		}

		fileScanner := bufio.NewScanner(f)

		fileScanner.Split(bufio.ScanLines)

		for fileScanner.Scan() {
			if totalFollows%1_000_000 == 0 {
				slog.Info("loaded follows", "total", totalFollows, "duration", time.Since(start))
			}

			followTxt := fileScanner.Text()
			parts := strings.Split(followTxt, ",") // actorDid,rkey,targetDid,createdAt,insertedAt
			if len(parts) < 3 {
				slog.Error("invalid follow", "follow", followTxt)
				continue
			}

			actorUID := graph.AcquireDID(parts[0])
			targetUID := graph.AcquireDID(parts[2])

			graph.AddFollow(actorUID, targetUID)

			totalFollows++
		}

		slog.Info("total follows", "total", totalFollows, "duration", time.Since(start))
	}

	e := echo.New()

	h := handlers.NewHandlers(graph)

	e.GET("/_health", h.Health)
	e.GET("/metrics", echo.WrapHandler(promhttp.Handler()))
	e.GET("/debug/*", echo.WrapHandler(http.DefaultServeMux))

	e.Use(echoprometheus.NewMiddleware("graphd"))

	e.GET("/followers", h.GetFollowers)
	e.GET("/following", h.GetFollowing)
	e.GET("/doesFollow", h.GetDoesFollow)

	e.POST("/follow", h.PostFollow)
	e.POST("/follows", h.PostFollows)

	e.POST("/unfollow", h.PostUnfollow)
	e.POST("/unfollows", h.PostUnfollows)

	return e.Start(fmt.Sprintf(":%d", cctx.Int("port")))
}
