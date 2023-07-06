package main

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"fmt"
	"log"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"syscall"
	"time"

	"net/http"
	_ "net/http/pprof"

	"github.com/bluesky-social/indigo/api"
	"github.com/bluesky-social/indigo/api/atproto"
	comatproto "github.com/bluesky-social/indigo/api/atproto"
	"github.com/bluesky-social/indigo/api/bsky"
	"github.com/bluesky-social/indigo/carstore"
	"github.com/bluesky-social/indigo/did"
	"github.com/bluesky-social/indigo/events"
	"github.com/bluesky-social/indigo/indexer"
	"github.com/labstack/echo-contrib/pprof"
	"github.com/multiformats/go-multibase"
	godid "github.com/whyrusleeping/go-did"
	"golang.org/x/crypto/acme/autocert"

	lexutil "github.com/bluesky-social/indigo/lex/util"
	"github.com/bluesky-social/indigo/repomgr"
	"github.com/bluesky-social/indigo/util"
	"github.com/gorilla/websocket"
	"github.com/labstack/echo/v4"
	"github.com/labstack/echo/v4/middleware"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"

	"go.uber.org/zap"
)

var supercolliderHost = "supercollider.jazco.io"

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

	em := events.NewEventManager(events.NewYoloPersister())

	s := &Server{
		events: em,
	}

	e := echo.New()

	e.AutoTLSManager.Cache = autocert.DirCache("/var/www/.cache")

	pprof.Register(e)

	e.Use(middleware.LoggerWithConfig(middleware.LoggerConfig{
		Format: "method=${method}, uri=${uri}, status=${status} latency=${latency_human}\n",
	}))

	e.GET("/", func(c echo.Context) error {
		return c.HTML(http.StatusOK, `<h1>Welcome to Supercollider!</h1>`)
	})

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

	repoman, privkey, err := initSpeedyRepoMan(ctx)
	if err != nil {
		log.Fatalf("failed to init repo manager: %+v\n", err)
	}

	multibaseKey, err := multibase.Encode(multibase.Base58BTC, privkey.Public().Raw.([]byte))
	if err != nil {
		log.Fatalf("failed to multibase encode key: %+v\n", err)
	}

	e.GET("/.well-known/did.json", func(c echo.Context) error {
		return c.JSON(http.StatusOK, map[string]any{
			"@context": []string{"https://www.w3.org/ns/did/v1"},
			"id":       "did:web:" + supercolliderHost,
			"alsoKnownAs": []string{
				"at://hello." + supercolliderHost,
			},
			"verificationMethod": []map[string]any{
				{
					"id":                 "#atproto",
					"type":               godid.KeyTypeP256,
					"controller":         "did:web:" + supercolliderHost,
					"publicKeyMultibase": multibaseKey,
				},
			},
			"service": []map[string]any{
				{
					"id":              "#atproto_pds",
					"type":            "AtprotoPersonalDataServer",
					"serviceEndpoint": "http://" + supercolliderHost,
				},
			},
		})
	})

	e.GET("/.well-known/atproto-did", func(c echo.Context) error {
		return c.String(http.StatusOK, "did:web:"+c.Request().Host)
	})

	// plcServer := api.PLCServer{
	// 	Host: "http://localhost:2582",
	// 	C:    http.DefaultClient,
	// }

	// statidDID, err := plcServer.CreateDID(ctx, privkey, "did:key:asdf", "hello.supercollider", "supercollider.jazco.io")
	// if err != nil {
	// 	log.Fatalf("failed to create did: %+v\n", err)
	// }

	staticDID := "did:web:hello." + supercolliderHost

	repoman.SetEventHandler(func(ctx context.Context, evt *repomgr.RepoEvent) {
		var outops []*comatproto.SyncSubscribeRepos_RepoOp
		for _, op := range evt.Ops {
			link := (*lexutil.LexLink)(op.RecCid)
			outops = append(outops, &comatproto.SyncSubscribeRepos_RepoOp{
				Path:   op.Collection + "/" + op.Rkey,
				Action: string(op.Kind),
				Cid:    link,
			})
		}

		toobig := false

		if err := em.AddEvent(ctx, &events.XRPCStreamEvent{
			RepoCommit: &comatproto.SyncSubscribeRepos_Commit{
				Repo:   staticDID,
				Prev:   (*lexutil.LexLink)(evt.OldRoot),
				Blocks: evt.RepoSlice,
				Commit: lexutil.LexLink(evt.NewRoot),
				Time:   time.Now().Format(util.ISO8601),
				Ops:    outops,
				TooBig: toobig,
				Rebase: evt.Rebase,
			},
			PrivUid: evt.User,
		}); err != nil {
			log.Errorf("failed to add event: %+v\n", err)
		}
	})

	// Create a control channel for the event emitter control messages
	// We want to produce events at around 80k/s since the socket can't handle much more than that
	evtControl := make(chan string, 1)
	go func() {
		running := false
		totalEmittedEvents := 0
		totalDesiredEvents := 500
		// limiter := rate.NewLimiter(rate.Limit(80_000), 100)

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
					totalEmittedEvents = 0
				}
			default:
				if !running {
					time.Sleep(time.Second)
					continue
				}

				if err := repoman.InitNewActor(ctx, 1, "hello."+supercolliderHost, staticDID, "catdog", "", ""); err != nil {
					log.Fatalf("failed to init actor: %+v\n", err)
				}

				for i := 0; i < totalDesiredEvents; i++ {
					totalEmittedEvents++
					if i%40_000 == 0 {
						log.Infof("emitted %d events\n", totalEmittedEvents)
					}

					// Wait for the limiter to allow us to emit another event
					// limiter.Wait(ctx)

					_, _, err = repoman.CreateRecord(ctx, 1, "app.bsky.feed.post", &bsky.FeedPost{
						Text: "cats",
					})
					if err != nil {
						log.Errorf("failed to create record: %+v\n", err)
					} else {
						eventsGeneratedCounter.Inc()
					}
					select {
					case <-ctx.Done():
						return
					default:
					}
				}
				log.Infof("emitted %d events, stopping\n", totalEmittedEvents)
				evtControl <- "stop"
				break
			}
		}
	}()

	e.GET("/xrpc/com.atproto.server.describeServer", s.DescribeServerHandler)
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

	port := "80"

	if err := e.Start(":" + port); err != nil {
		log.Errorf("failed to start server: %+v\n", err)
	}
}

func (s *Server) EventsHandler(c echo.Context) error {
	conn, err := websocket.Upgrade(c.Response().Writer, c.Request(), c.Response().Header(), 1<<10, 1<<10)
	if err != nil {
		return err
	}

	var cursor *int64

	if c.QueryParam("cursor") != "" {
		cursorFromQuery, err := strconv.ParseInt(c.QueryParam("cursor"), 10, 64)
		if err != nil {
			return err
		}
		cursor = &cursorFromQuery
	}

	ctx := c.Request().Context()

	evts, cancel, err := s.events.Subscribe(ctx, func(evt *events.XRPCStreamEvent) bool {
		return true
	}, cursor)
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

func (s *Server) DescribeServerHandler(c echo.Context) error {
	invcode := false
	resp := &atproto.ServerDescribeServer_Output{
		InviteCodeRequired:   &invcode,
		AvailableUserDomains: []string{},
		Links:                &atproto.ServerDescribeServer_Links{},
	}
	return c.JSON(http.StatusOK, resp)
}

func setupDb(p string) (*gorm.DB, error) {
	db, err := gorm.Open(sqlite.Open(p))
	if err != nil {
		return nil, fmt.Errorf("failed to open db: %w", err)
	}

	if err := db.Exec(`PRAGMA journal_mode=WAL;
		pragma synchronous = normal;
		pragma temp_store = memory;
		pragma mmap_size = 30000000000;`,
	).Error; err != nil {
		return nil, fmt.Errorf("failed to set pragma modes: %w", err)
	}

	return db, nil
}

func initSpeedyRepoMan(ctx context.Context) (*repomgr.RepoManager, *godid.PrivKey, error) {
	dir, err := os.MkdirTemp("", "supercollider")
	if err != nil {
		return nil, nil, err
	}

	cardb, err := setupDb("file::memory:?cache=shared")
	if err != nil {
		return nil, nil, err
	}

	cspath := filepath.Join(dir, "carstore")
	if err := os.Mkdir(cspath, 0775); err != nil {
		return nil, nil, err
	}

	cs, err := carstore.NewCarStore(cardb, cspath)
	if err != nil {
		return nil, nil, err
	}

	hs := repomgr.NewMemHeadStore()

	mr := did.NewMultiResolver()
	mr.AddHandler("plc", &api.PLCServer{
		Host: "http://localhost:2582",
	})
	mr.AddHandler("web", &did.WebResolver{})

	privateKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return nil, nil, err
	}

	key := godid.PrivKey{
		Type: godid.KeyTypeP256,
		Raw:  privateKey,
	}

	kmgr := indexer.NewKeyManager(mr, &key)

	repoman := repomgr.NewRepoManager(hs, cs, kmgr)

	return repoman, &key, nil
}
