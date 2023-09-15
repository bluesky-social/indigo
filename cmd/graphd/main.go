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
			Value: 8345,
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

type HealthStatus struct {
	Status  string `json:"status"`
	Version string `json:"version"`
	Message string `json:"msg,omitempty"`
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

	e.GET("/_health", func(c echo.Context) error {
		return c.JSON(200, HealthStatus{
			Status:  "ok",
			Version: version.Version,
		})
	})
	e.GET("/metrics", echo.WrapHandler(promhttp.Handler()))
	e.GET("/debug/*", echo.WrapHandler(http.DefaultServeMux))

	e.Use(echoprometheus.NewMiddleware("graphd"))

	e.GET("/followers", func(c echo.Context) error {
		did := c.QueryParam("did")
		queryStart := time.Now()

		uid, ok := graph.GetUID(did)
		if !ok {
			slog.Error("uid not found")
			return c.JSON(404, "uid not found")
		}

		uidDone := time.Now()
		followers, err := graph.GetFollowers(uid)
		if err != nil {
			slog.Error("failed to get followers", "err", err)
		}
		membersDone := time.Now()

		dids, err := graph.GetDIDs(followers)
		if err != nil {
			slog.Error("failed to get dids", "err", err)
		}

		didsDone := time.Now()
		slog.Debug("got followers",
			"followers", len(followers),
			"uid", uid,
			"uidDuration", uidDone.Sub(queryStart),
			"membersDuration", membersDone.Sub(uidDone),
			"didsDuration", didsDone.Sub(membersDone),
			"totalDuration", didsDone.Sub(queryStart),
		)

		return c.JSON(200, dids)
	})

	e.GET("/following", func(c echo.Context) error {
		did := c.QueryParam("did")
		queryStart := time.Now()

		uid, ok := graph.GetUID(did)
		if !ok {
			slog.Error("uid not found")
			return c.JSON(404, "uid not found")
		}

		uidDone := time.Now()
		following, err := graph.GetFollowing(uid)
		if err != nil {
			slog.Error("failed to get following", "err", err)
		}
		membersDone := time.Now()

		dids, err := graph.GetDIDs(following)
		if err != nil {
			slog.Error("failed to get dids", "err", err)
		}

		didsDone := time.Now()
		slog.Debug("got following",
			"following", len(following),
			"uid", uid,
			"uidDuration", uidDone.Sub(queryStart),
			"membersDuration", membersDone.Sub(uidDone),
			"didsDuration", didsDone.Sub(membersDone),
			"totalDuration", didsDone.Sub(queryStart),
		)

		return c.JSON(200, dids)
	})

	e.GET("/doesFollow", func(c echo.Context) error {
		actorDid := c.QueryParam("actorDid")
		targetDid := c.QueryParam("targetDid")

		start := time.Now()

		actorUID, ok := graph.GetUID(actorDid)
		if !ok {
			slog.Error("actor uid not found")
			return c.JSON(404, "actor uid not found")
		}

		targetUID, ok := graph.GetUID(targetDid)
		if !ok {
			slog.Error("target uid not found")
			return c.JSON(404, "target uid not found")
		}

		doesFollow, err := graph.DoesFollow(actorUID, targetUID)
		if err != nil {
			slog.Error("failed to check if follows", "err", err)
			return c.JSON(500, "failed to check if follows")
		}

		slog.Debug("checked if follows", "doesFollow", doesFollow, "duration", time.Since(start))

		return c.JSON(200, doesFollow)
	})

	return e.Start(fmt.Sprintf(":%d", cctx.Int("port")))
}
