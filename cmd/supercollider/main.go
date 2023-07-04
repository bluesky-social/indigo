package main

import (
	"context"
	"fmt"
	"log"
	"net/url"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/bluesky-social/indigo/api/atproto"
	"github.com/bluesky-social/indigo/events"
	lexutil "github.com/bluesky-social/indigo/lex/util"
	"github.com/gorilla/websocket"
	"github.com/labstack/echo/v4"
	"github.com/labstack/echo/v4/middleware"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"github.com/prometheus/client_golang/prometheus/promhttp"

	"go.uber.org/zap"
)

var eventsGeneratedCounter = promauto.NewCounter(prometheus.CounterOpts{
	Name: "supercollider_events_generated_total",
	Help: "The total number of events generated",
})

var eventsSentCounter = promauto.NewCounter(prometheus.CounterOpts{
	Name: "supercollider_events_sent_total",
	Help: "The total number of events sent",
})

type Server struct {
	events *events.EventManager
}

func main() {
	ctx := context.Background()
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	// Trap SIGINT to trigger a shutdown.
	signals := make(chan os.Signal, 1)
	signal.Notify(signals, syscall.SIGINT, syscall.SIGTERM)

	go func() {
		select {
		case <-signals:
			cancel()
			fmt.Println("shutting down on signal")
			// Give the server some time to shutdown gracefully, then exit.
			time.Sleep(time.Second * 5)
			os.Exit(0)
		case <-ctx.Done():
			fmt.Println("shutting down on context done")
		}
	}()

	rawlog, err := zap.NewProduction()
	if err != nil {
		log.Fatalf("failed to create logger: %+v\n", err)
	}
	defer func() {
		log.Printf("main function teardown\n")
		err := rawlog.Sync()
		if err != nil {
			log.Printf("failed to sync logger on teardown: %+v", err.Error())
		}
	}()

	log := rawlog.Sugar().With("source", "supercollider_main")

	log.Info("starting supercollider")

	em := events.NewEventManager(events.NewMemPersister())

	s := &Server{
		events: em,
	}

	e := echo.New()

	e.Use(middleware.LoggerWithConfig(middleware.LoggerConfig{
		Format: "method=${method}, uri=${uri}, status=${status} latency=${latency_human}\n",
	}))

	e.HTTPErrorHandler = func(err error, ctx echo.Context) {
		switch err := err.(type) {
		case *echo.HTTPError:
			if err2 := ctx.JSON(err.Code, map[string]any{
				"error": err.Message,
			}); err2 != nil {
				log.Errorf("Failed to write http error: %s", err2)
			}
		default:
			sendHeader := true
			if ctx.Path() == "/xrpc/com.atproto.sync.subscribeRepos" {
				sendHeader = false
			}

			log.Warnf("HANDLER ERROR: (%s) %s", ctx.Path(), err)

			if sendHeader {
				ctx.Response().WriteHeader(500)
			}
		}
	}

	testCommit, err := acquireCommitFromProdFirehose(ctx)
	if err != nil {
		log.Fatalf("failed to acquire test commit: %+v\n", err)
	}

	log.Infof("testCommit: %s | %s\n", testCommit.Repo, testCommit.Ops[0].Path)

	// Create a control channel for the event emitter control messages
	evtControl := make(chan string, 1)
	go func() {
		running := false
		for {
			select {
			case <-ctx.Done():
				return
			case cmd := <-evtControl:
				switch cmd {
				case "start":
					running = true
				case "stop":
					running = false
				}
			default:
				if !running {
					time.Sleep(time.Second)
					continue
				}
				eventCount := 10_000
				for i := 0; i < eventCount; i++ {
					eventsGeneratedCounter.Inc()
					ops := []*atproto.SyncSubscribeRepos_RepoOp{}
					for _, op := range testCommit.Ops {
						ops = append(ops, &atproto.SyncSubscribeRepos_RepoOp{
							Action: op.Action,
							Cid:    op.Cid,
							Path:   op.Path,
						})
					}

					commit := &atproto.SyncSubscribeRepos_Commit{
						Seq:    testCommit.Seq + int64(i),
						Blobs:  testCommit.Blobs,
						Blocks: testCommit.Blocks,
						Prev:   testCommit.Prev,
						Commit: testCommit.Commit,
						Rebase: testCommit.Rebase,
						Repo:   testCommit.Repo,
						Ops:    ops,
						Time:   testCommit.Time,
						TooBig: testCommit.TooBig,
					}

					em.AddEvent(ctx, &events.XRPCStreamEvent{
						RepoCommit: commit,
					})
				}
				log.Infof("added %d events\n", eventCount)
				time.Sleep(time.Millisecond * 10)
			}
		}
	}()

	e.GET("/xrpc/com.atproto.sync.subscribeRepos", s.EventsHandler)
	e.GET("/metrics", echo.WrapHandler(promhttp.Handler()))
	e.GET("/generate", func(c echo.Context) error {
		evtControl <- "start"
		return c.String(200, "stream started")
	})
	e.GET("/stop", func(c echo.Context) error {
		evtControl <- "stop"
		return c.String(200, "stream stopped")
	})

	port := "12832"

	if err := e.Start(":" + port); err != nil {
		log.Errorf("failed to start server: %+v\n", err)
	}
}

func acquireCommitFromProdFirehose(ctx context.Context) (*atproto.SyncSubscribeRepos_Commit, error) {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	u := url.URL{Scheme: "wss", Host: "bsky.social", Path: "/xrpc/com.atproto.sync.subscribeRepos"}
	c, _, err := websocket.DefaultDialer.Dial(u.String(), nil)
	if err != nil {
		return nil, fmt.Errorf("failed to connect to websocket: %w", err)
	}
	defer c.Close()

	var commit *atproto.SyncSubscribeRepos_Commit

	sched := events.SequentialScheduler{
		Do: func(ctx context.Context, evt *events.XRPCStreamEvent) error {
			switch {
			case evt.RepoCommit != nil:
				commit = evt.RepoCommit
				return nil
			}
			return nil
		},
	}

	go events.HandleRepoStream(ctx, c, &sched)

	// Wait in a loop for the commit to be populated, then cancel the context and return the commit
	for {
		if commit != nil {
			return commit, nil
		}
		time.Sleep(time.Millisecond * 100)
	}
}

func (s *Server) EventsHandler(c echo.Context) error {
	conn, err := websocket.Upgrade(c.Response().Writer, c.Request(), c.Response().Header(), 1<<10, 1<<10)
	if err != nil {
		return err
	}

	ctx := c.Request().Context()

	evts, cancel, err := s.events.Subscribe(ctx, func(evt *events.XRPCStreamEvent) bool {
		return true
	}, nil)
	if err != nil {
		return err
	}
	defer cancel()

	header := events.EventHeader{Op: events.EvtKindMessage}
	for evt := range evts {
		wc, err := conn.NextWriter(websocket.BinaryMessage)
		if err != nil {
			return err
		}

		var obj lexutil.CBOR

		switch {
		case evt.Error != nil:
			header.Op = events.EvtKindErrorFrame
			obj = evt.Error
		case evt.RepoCommit != nil:
			header.MsgType = "#commit"
			obj = evt.RepoCommit
		case evt.RepoHandle != nil:
			header.MsgType = "#handle"
			obj = evt.RepoHandle
		case evt.RepoInfo != nil:
			header.MsgType = "#info"
			obj = evt.RepoInfo
		case evt.RepoMigrate != nil:
			header.MsgType = "#migrate"
			obj = evt.RepoMigrate
		case evt.RepoTombstone != nil:
			header.MsgType = "#tombstone"
			obj = evt.RepoTombstone
		default:
			return fmt.Errorf("unrecognized event kind")
		}

		if err := header.MarshalCBOR(wc); err != nil {
			return fmt.Errorf("failed to write header: %w", err)
		}

		if err := obj.MarshalCBOR(wc); err != nil {
			return fmt.Errorf("failed to write event: %w", err)
		}

		if err := wc.Close(); err != nil {
			return fmt.Errorf("failed to flush-close our event write: %w", err)
		}

		eventsSentCounter.Inc()
	}

	return nil
}
