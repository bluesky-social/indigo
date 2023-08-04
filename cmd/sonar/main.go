package main

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"

	"github.com/bluesky-social/indigo/events"
	"github.com/bluesky-social/indigo/events/schedulers/autoscaling"
	"github.com/bluesky-social/indigo/sonar"
	"github.com/bluesky-social/indigo/util/version"
	"github.com/gorilla/websocket"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	_ "go.uber.org/automaxprocs"
	"go.uber.org/zap"

	"github.com/urfave/cli/v2"
)

func main() {
	app := cli.App{
		Name:    "sonar",
		Usage:   "atproto firehose monitoring tool",
		Version: version.Version,
	}

	app.Flags = []cli.Flag{
		&cli.StringFlag{
			Name:  "ws-url",
			Usage: "full websocket path to the ATProto SubscribeRepos XRPC endpoint",
			Value: "wss://bsky.social/xrpc/com.atproto.sync.subscribeRepos",
		},
		&cli.StringFlag{
			Name:  "log-level",
			Usage: "log level",
			Value: "info",
		},
		&cli.IntFlag{
			Name:  "port",
			Usage: "listen port for metrics server",
			Value: 8345,
		},
		&cli.IntFlag{
			Name:  "worker-count",
			Usage: "number of workers to process events",
			Value: 10,
		},
		&cli.IntFlag{
			Name:  "max-queue-size",
			Usage: "max number of events to queue",
			Value: 10,
		},
		&cli.StringFlag{
			Name:  "cursor-file",
			Usage: "path to cursor file",
			Value: "sonar_cursor.json",
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

	rawlog, err := zap.NewProduction()
	if err != nil {
		log.Fatalf("failed to create logger: %+v", err)
	}
	defer func() {
		log.Printf("main function teardown")
		err := rawlog.Sync()
		if err != nil {
			log.Printf("failed to sync logger on teardown: %+v", err.Error())
		}
	}()

	log := rawlog.Sugar().With("source", "sonar_main")

	log.Info("starting sonar")

	u, err := url.Parse(cctx.String("ws-url"))
	if err != nil {
		log.Fatalf("failed to parse ws-url: %+v", err)
	}

	s, err := sonar.NewSonar(log, cctx.String("cursor-file"), u.String())
	if err != nil {
		log.Fatalf("failed to create sonar: %+v", err)
	}

	wg := sync.WaitGroup{}

	scalingSettings := autoscaling.DefaultAutoscaleSettings()
	scalingSettings.MaxConcurrency = cctx.Int("worker-count")
	scalingSettings.AutoscaleFrequency = time.Second

	pool := autoscaling.NewScheduler(scalingSettings, u.Host, s.HandleStreamEvent)

	// Start a goroutine to manage the cursor file, saving the current cursor every 5 seconds.
	go func() {
		wg.Add(1)
		defer wg.Done()
		ticker := time.NewTicker(5 * time.Second)
		rawlog, err := zap.NewProduction()
		if err != nil {
			log.Fatalf("failed to create logger: %+v", err)
		}
		log := rawlog.Sugar().With("source", "cursor_file_manager")

		for {
			select {
			case <-ctx.Done():
				log.Info("shutting down cursor file manager")
				err := s.WriteCursorFile()
				if err != nil {
					log.Errorf("failed to write cursor file: %+v", err)
				}
				log.Info("cursor file manager shut down successfully")
				return
			case <-ticker.C:
				err := s.WriteCursorFile()
				if err != nil {
					log.Errorf("failed to write cursor file: %+v", err)
				}
			}
		}
	}()

	// Start a goroutine to manage the liveness checker, shutting down if no events are received for 15 seconds
	go func() {
		wg.Add(1)
		defer wg.Done()
		ticker := time.NewTicker(15 * time.Second)
		lastSeq := int64(0)

		rawlog, err := zap.NewProduction()
		if err != nil {
			log.Fatalf("failed to create logger: %+v", err)
		}
		log := rawlog.Sugar().With("source", "liveness_checker")

		for {
			select {
			case <-ctx.Done():
				log.Info("shutting down liveness checker")
				return
			case <-ticker.C:
				s.ProgMux.Lock()
				seq := s.Progress.LastSeq
				s.ProgMux.Unlock()
				if seq <= lastSeq {
					log.Errorf("no new events in last 15 seconds, shutting down for docker to restart me")
					cancel()
				} else {
					log.Infof("last event sequence: %d", seq)
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
	go func() {
		wg.Add(1)
		defer wg.Done()
		rawlog, err := zap.NewProduction()
		if err != nil {
			log.Fatalf("failed to create logger: %+v", err)
		}
		log := rawlog.Sugar().With("source", "metrics_server")

		log.Infof("metrics server listening on port %d", cctx.Int("port"))

		if err := metricServer.ListenAndServe(); err != http.ErrServerClosed {
			log.Fatalf("failed to start metrics server: %+v", err)
		}
		log.Info("metrics server shut down successfully")
	}()

	if s.Progress.LastSeq >= 0 {
		u.RawQuery = fmt.Sprintf("cursor=%d", s.Progress.LastSeq)
	}

	log.Infof("connecting to WebSocket at: %s", u.String())
	c, _, err := websocket.DefaultDialer.Dial(u.String(), http.Header{
		"User-Agent": []string{"sonar/1.0"},
	})
	if err != nil {
		log.Infof("failed to connect to websocket: %v", err)
		return err
	}
	defer c.Close()

	go func() {
		wg.Add(1)
		defer wg.Done()
		err = events.HandleRepoStream(ctx, c, pool)
		log.Infof("HandleRepoStream returned unexpectedly: %+v...", err)
		cancel()
	}()

	select {
	case <-signals:
		cancel()
		fmt.Println("shutting down on signal")
	case <-ctx.Done():
		fmt.Println("shutting down on context done")
	}

	log.Info("shutting down, waiting for workers to clean up...")

	if err := metricServer.Shutdown(ctx); err != nil {
		log.Errorf("failed to shut down metrics server: %+v", err)
		wg.Done()
	}

	wg.Wait()
	log.Info("shut down successfully")

	return nil
}
