package main

import (
	"context"
	"fmt"
	"log"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"

	"github.com/bluesky-social/indigo/events"
	"github.com/bluesky-social/indigo/events/schedulers/sequential"
	"github.com/bluesky-social/indigo/sonar"
	"github.com/gorilla/websocket"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	_ "go.uber.org/automaxprocs"

	"github.com/carlmjohnson/versioninfo"
	"github.com/urfave/cli/v2"
)

func main() {
	app := cli.App{
		Name:    "sonar",
		Usage:   "atproto firehose monitoring tool",
		Version: versioninfo.Short(),
	}

	app.Flags = []cli.Flag{
		&cli.StringFlag{
			Name:    "ws-url",
			Usage:   "full websocket path to the ATProto SubscribeRepos XRPC endpoint",
			Value:   "wss://bsky.network/xrpc/com.atproto.sync.subscribeRepos",
			EnvVars: []string{"SONAR_WS_URL"},
		},
		&cli.StringFlag{
			Name:    "log-level",
			Usage:   "log level",
			Value:   "info",
			EnvVars: []string{"SONAR_LOG_LEVEL"},
		},
		&cli.IntFlag{
			Name:    "port",
			Usage:   "listen port for metrics server",
			Value:   8345,
			EnvVars: []string{"SONAR_PORT"},
		},
		&cli.IntFlag{
			Name:  "max-queue-size",
			Usage: "max number of events to queue",
			Value: 10,
		},
		&cli.StringFlag{
			Name:    "cursor-file",
			Usage:   "path to cursor file",
			Value:   "sonar_cursor.json",
			EnvVars: []string{"SONAR_CURSOR_FILE"},
		},
	}

	app.Action = Sonar

	err := app.Run(os.Args)
	if err != nil {
		log.Fatal(err)
	}
}

func Sonar(cctx *cli.Context) error {
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

	logger = logger.With("source", "sonar_main")
	logger.Info("starting sonar")

	u, err := url.Parse(cctx.String("ws-url"))
	if err != nil {
		log.Fatalf("failed to parse ws-url: %+v", err)
	}

	s, err := sonar.NewSonar(logger, cctx.String("cursor-file"), u.String())
	if err != nil {
		log.Fatalf("failed to create sonar: %+v", err)
	}

	wg := sync.WaitGroup{}

	pool := sequential.NewScheduler(u.Host, s.HandleStreamEvent)

	// Start a goroutine to manage the cursor file, saving the current cursor every 5 seconds.
	wg.Add(1)
	go func() {
		defer wg.Done()
		ticker := time.NewTicker(5 * time.Second)
		logger := logger.With("source", "cursor_file_manager")

		for {
			select {
			case <-ctx.Done():
				logger.Info("shutting down cursor file manager")
				err := s.WriteCursorFile()
				if err != nil {
					logger.Error("failed to write cursor file", "err", err)
				}
				logger.Info("cursor file manager shut down successfully")
				return
			case <-ticker.C:
				err := s.WriteCursorFile()
				if err != nil {
					logger.Error("failed to write cursor file", "err", err)
				}
			}
		}
	}()

	// Start a goroutine to manage the liveness checker, shutting down if no events are received for 15 seconds
	wg.Add(1)
	go func() {
		defer wg.Done()
		ticker := time.NewTicker(15 * time.Second)
		lastSeq := int64(0)

		logger = logger.With("source", "liveness_checker")

		for {
			select {
			case <-ctx.Done():
				logger.Info("shutting down liveness checker")
				return
			case <-ticker.C:
				s.ProgMux.Lock()
				seq := s.Progress.LastSeq
				s.ProgMux.Unlock()
				if seq <= lastSeq {
					logger.Error("no new events in last 15 seconds, shutting down for docker to restart me")
					cancel()
				} else {
					logger.Info("last event sequence", "seq", seq)
					lastSeq = seq
				}
			}
		}
	}()

	mux := http.NewServeMux()
	mux.Handle("/metrics", promhttp.Handler())

	metricServer := &http.Server{
		Addr:    fmt.Sprintf(":%d", cctx.Int("port")),
		Handler: mux,
	}

	// Startup metrics server
	wg.Add(1)
	go func() {
		defer wg.Done()
		logger = logger.With("source", "metrics_server")

		logger.Info("metrics server listening", "port", cctx.Int("port"))

		if err := metricServer.ListenAndServe(); err != http.ErrServerClosed {
			log.Fatalf("failed to start metrics server: %+v", err)
		}
		logger.Info("metrics server shut down successfully")
	}()

	if s.Progress.LastSeq >= 0 {
		u.RawQuery = fmt.Sprintf("cursor=%d", s.Progress.LastSeq)
	}

	logger.Info("connecting to WebSocket", "url", u.String())
	c, _, err := websocket.DefaultDialer.Dial(u.String(), http.Header{
		"User-Agent": []string{"sonar/1.1"},
	})
	if err != nil {
		logger.Info("failed to connect to websocket", "err", err)
		return err
	}
	defer c.Close()

	wg.Add(1)
	go func() {
		defer wg.Done()
		err = events.HandleRepoStream(ctx, c, pool, logger)
		logger.Info("HandleRepoStream returned unexpectedly", "err", err)
		cancel()
	}()

	select {
	case <-signals:
		cancel()
		fmt.Println("shutting down on signal")
	case <-ctx.Done():
		fmt.Println("shutting down on context done")
	}

	logger.Info("shutting down, waiting for workers to clean up")

	if err := metricServer.Shutdown(ctx); err != nil {
		logger.Error("failed to shut down metrics server", "err", err)
		wg.Done()
	}

	wg.Wait()
	logger.Info("shut down successfully")

	return nil
}
