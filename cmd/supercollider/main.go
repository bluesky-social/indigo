package main

import (
	"context"
	"crypto/rand"
	"fmt"
	"log"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"net/http"
	_ "net/http/pprof"

	"github.com/bluesky-social/indigo/api/atproto"
	comatproto "github.com/bluesky-social/indigo/api/atproto"
	"github.com/bluesky-social/indigo/api/bsky"
	"github.com/bluesky-social/indigo/carstore"
	"github.com/bluesky-social/indigo/did"
	"github.com/bluesky-social/indigo/events"
	"github.com/bluesky-social/indigo/indexer"
	"github.com/bluesky-social/indigo/models"
	"github.com/bluesky-social/indigo/plc"
	"github.com/bluesky-social/indigo/util/version"
	petname "github.com/dustinkirkland/golang-petname"
	"github.com/labstack/echo-contrib/pprof"
	"github.com/urfave/cli/v2"
	godid "github.com/whyrusleeping/go-did"
	"golang.org/x/crypto/acme/autocert"
	"golang.org/x/time/rate"

	lexutil "github.com/bluesky-social/indigo/lex/util"
	"github.com/bluesky-social/indigo/repomgr"
	"github.com/bluesky-social/indigo/util"
	"github.com/gorilla/websocket"
	"github.com/labstack/echo/v4"
	"github.com/labstack/echo/v4/middleware"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"gorm.io/driver/postgres"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"

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
	Events       *events.EventManager
	Dids         []string
	Host         string
	EnableSSL    bool
	Logger       *zap.SugaredLogger
	EventControl chan string
	MultibaseKey string
	RepoManager  *repomgr.RepoManager

	// Event Loop Parameters
	TotalDesiredEvents int
	MaxEventsPerSecond int
}

func main() {
	ctx := context.Background()
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	app := cli.App{
		Name:    "supercollider",
		Usage:   "atproto event noise-maker for BGS load testing",
		Version: version.Version,
	}

	app.Flags = []cli.Flag{
		&cli.StringFlag{
			Name:    "hostname",
			Usage:   "hostname of this server (forward *.hostname DNS records to this server)",
			Value:   "supercollider.jazco.io",
			EnvVars: []string{"SUPERCOLLIDER_HOST"},
		},
		&cli.StringFlag{
			Name:    "postgres-url",
			Usage:   "postgres connection string for CarDB (if not set, will use sqlite in-memory)",
			EnvVars: []string{"SUPERCOLLIDER_POSTGRES_URL"},
		},
		&cli.BoolFlag{
			Name:  "use-ssl",
			Usage: "listen on port 443 and use SSL (needs to be run as root and have external DNS setup)",
			Value: false,
		},
		&cli.IntFlag{
			Name:  "port",
			Usage: "port for the HTTP(S) server to listen on (defaults to 80 if not using SSL, 443 if using SSL)",
		},
		&cli.IntFlag{
			Name:  "num-users",
			Usage: "number of fake users to produce events for",
			Value: 100,
		},
		&cli.IntFlag{
			Name:  "events-per-second",
			Usage: "maximum number of events to generate per second",
			Value: 300,
		},
		&cli.IntFlag{
			Name:  "total-events-per-loop",
			Usage: "total number of events to generate per loop",
			Value: 1_000_000,
		},
	}

	app.Action = Supercollider

	err := app.Run(os.Args)
	if err != nil {
		log.Fatal(err)
	}
}

func Supercollider(cctx *cli.Context) error {
	ctx := cctx.Context
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

	rawlog, err := zap.NewDevelopment()
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

	// Configure the repomanager and keypair for our fake accounts
	repoman, privkey, err := initSpeedyRepoMan(cctx.String("postgres-url"))
	if err != nil {
		log.Fatalf("failed to init repo manager: %+v\n", err)
	}

	vMethod, err := godid.VerificationMethodFromKey(privkey.Public())
	if err != nil {
		log.Fatalf("failed to generate verification method: %+v\n", err)
	}

	// Initialize fake account DIDs
	dids := []string{}
	for i := 0; i < cctx.Int("num-users"); i++ {
		did := fmt.Sprintf("did:web:%s.%s", petname.Generate(4, "-"), cctx.String("hostname"))
		dids = append(dids, did)
	}

	// Instantiate Server
	s := &Server{
		Logger:    log,
		EnableSSL: cctx.Bool("use-ssl"),
		Host:      cctx.String("hostname"),

		RepoManager:  repoman,
		MultibaseKey: *vMethod.PublicKeyMultibase,
		Dids:         dids,

		Events:             em,
		EventControl:       make(chan string),
		TotalDesiredEvents: cctx.Int("total-events-per-loop"),
		MaxEventsPerSecond: cctx.Int("events-per-second"),
	}

	// HTTP Server setup and Middleware Plumbing
	e := echo.New()
	e.AutoTLSManager.Cache = autocert.DirCache("/var/www/.cache")
	pprof.Register(e)
	e.Use(middleware.LoggerWithConfig(middleware.LoggerConfig{
		Format: "method=${method}, ip=${remote_ip}, uri=${uri}, status=${status} latency=${latency_human} (ua=${user_agent})\n",
	}))

	// Configure the HTTP Error Handler to support Websocket errors
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

	e.GET("/", func(c echo.Context) error {
		return c.HTML(http.StatusOK, `<h1>Welcome to Supercollider!</h1>`)
	})

	repoman.SetEventHandler(s.HandleRepoEvent)

	e.GET("/.well-known/did.json", s.HandleWellKnownDid)
	e.GET("/.well-known/atproto-did", s.HandleAtprotoDid)
	e.GET("/xrpc/com.atproto.server.describeServer", s.DescribeServerHandler)
	e.GET("/xrpc/com.atproto.sync.subscribeRepos", s.HandleSubscribeRepos)
	e.GET("/metrics", echo.WrapHandler(promhttp.Handler()))
	e.GET("/generate", s.HandleStartGenerating)
	e.GET("/stop", s.HandleStopGenerating)

	port := cctx.Int("port")
	if port == 0 {
		if cctx.Bool("use-ssl") {
			port = 443
		} else {
			port = 80
		}
	}

	go s.EventGenerationLoop(ctx, cctx.String("postgres-url") != "")

	listenAddress := fmt.Sprintf(":%d", port)
	if cctx.Bool("use-ssl") {
		err = e.StartAutoTLS(listenAddress)
	} else {
		err = e.Start(listenAddress)
	}
	if err != nil {
		log.Errorf("failed to start server: %+v\n", err)
	}
	return nil
}

// Configure a gorm SQLite DB with some sensible defaults
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

// Configure a Postgres SqliteDB
func setupPostgresDb(p string) (*gorm.DB, error) {
	db, err := gorm.Open(postgres.Open(p), &gorm.Config{})
	if err != nil {
		return nil, fmt.Errorf("failed to open db: %w", err)
	}

	return db, nil
}

// Stand up a Repo Manager with a Web DID Resolver
func initSpeedyRepoMan(postgresString string) (*repomgr.RepoManager, *godid.PrivKey, error) {
	dir, err := os.MkdirTemp("", "supercollider")
	if err != nil {
		return nil, nil, err
	}

	var cardb *gorm.DB
	if postgresString != "" {
		cardb, err = setupPostgresDb(postgresString)
		if err != nil {
			return nil, nil, err
		}
	} else {
		cardb, err = setupDb("file::memory:?cache=shared")
		if err != nil {
			return nil, nil, err
		}
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
	mr.AddHandler("web", &did.WebResolver{
		Insecure: true,
	})

	cachedidr := plc.NewCachingDidResolver(mr, time.Minute*5, 1000)

	key, err := godid.GeneratePrivKey(rand.Reader, godid.KeyTypeSecp256k1)
	if err != nil {
		return nil, nil, err
	}

	kmgr := indexer.NewKeyManager(cachedidr, key)

	repoman := repomgr.NewRepoManager(hs, cs, kmgr)

	return repoman, key, nil
}

// HandleRepoEvent is the callback for the RepoManager
func (s *Server) HandleRepoEvent(ctx context.Context, evt *repomgr.RepoEvent) {
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

	if err := s.Events.AddEvent(ctx, &events.XRPCStreamEvent{
		RepoCommit: &comatproto.SyncSubscribeRepos_Commit{
			Repo:   s.Dids[evt.User-1],
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
		s.Logger.Errorf("failed to add event: %+v\n", err)
	}
}

// Event Generation Loop and Control

// EventGenerationLoop is the main loop for generating events
func (s *Server) EventGenerationLoop(ctx context.Context, concurrent bool) {
	running := false
	totalEmittedEvents := 0

	// We want to produce events at a maximum rate to prevent the buffer from overrunning
	limiter := rate.NewLimiter(rate.Limit(s.MaxEventsPerSecond), 10)

	s.Logger.Infof("starting event generation loop with %d desired events and %d evt/s maximum\n",
		s.TotalDesiredEvents, s.MaxEventsPerSecond,
	)

	for {
		select {
		case <-ctx.Done():
			return
		case cmd := <-s.EventControl:
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
			for i, did := range s.Dids {
				uid := models.Uid(i + 1)
				if err := s.RepoManager.InitNewActor(ctx, uid, strings.TrimPrefix(did, "did:web:"), did, "catdog", "", ""); err != nil {
					log.Fatalf("failed to init actor: %+v\n", err)
				}
			}

			if concurrent {
				recordsPerActor := s.TotalDesiredEvents / len(s.Dids)
				wg := sync.WaitGroup{}
				for i := 0; i < len(s.Dids); i++ {
					wg.Add(1)
					go func(i int) {
						for j := 0; j < recordsPerActor; j++ {
							limiter.Wait(ctx)
							_, _, err := s.RepoManager.CreateRecord(ctx, models.Uid(i+1), "app.bsky.feed.post", &bsky.FeedPost{
								Text: "cats",
							})
							if err != nil {
								s.Logger.Errorf("failed to create record: %+v\n", err)
							} else {
								eventsGeneratedCounter.Inc()
							}
							select {
							case <-ctx.Done():
								return
							default:
							}
						}
					}(i)
				}
				wg.Wait()
			} else {
				for i := 0; i < s.TotalDesiredEvents; i++ {
					limiter.Wait(ctx)
					_, _, err := s.RepoManager.CreateRecord(ctx, models.Uid(i%len(s.Dids)+1), "app.bsky.feed.post", &bsky.FeedPost{
						Text: "cats",
					})
					if err != nil {
						s.Logger.Errorf("failed to create record: %+v\n", err)
					} else {
						eventsGeneratedCounter.Inc()
					}
					select {
					case <-ctx.Done():
						return
					default:
					}
				}
			}

			s.Logger.Infof("emitted %d events, stopping\n", totalEmittedEvents)
			s.EventControl <- "stop"
			break
		}
	}
}

// HandleStartGenerating starts the event generation loop
func (s *Server) HandleStartGenerating(ctx echo.Context) error {
	s.EventControl <- "start"
	return ctx.String(200, "stream started")
}

// HandleStopGenerating stops the event generation loop
func (s *Server) HandleStopGenerating(ctx echo.Context) error {
	s.EventControl <- "stop"
	return ctx.String(200, "stream stopped")
}

// ATProto Handlers for DID Web

// HandleAtprotoDid handles reverse-lookups (handle -> DID)
func (s *Server) HandleAtprotoDid(c echo.Context) error {
	return c.String(http.StatusOK, "did:web:"+c.Request().Host)
}

// HandleWellKnownDid handles DID document lookups (DID -> identity)
func (s *Server) HandleWellKnownDid(c echo.Context) error {
	return c.JSON(http.StatusOK, map[string]any{
		"@context": []string{"https://www.w3.org/ns/did/v1"},
		"id":       "did:web:" + c.Request().Host,
		"alsoKnownAs": []string{
			"at://" + c.Request().Host,
		},
		"verificationMethod": []map[string]any{
			{
				"id":                 "#atproto",
				"type":               godid.KeyTypeSecp256k1,
				"controller":         "did:web:" + s.Host,
				"publicKeyMultibase": s.MultibaseKey,
			},
		},
		"service": []map[string]any{
			{
				"id":              "#atproto_pds",
				"type":            "AtprotoPersonalDataServer",
				"serviceEndpoint": "http://" + s.Host,
			},
		},
	})
}

// DescribeServerHandler identifies the server as a PDS (even though it isn't)
func (s *Server) DescribeServerHandler(c echo.Context) error {
	invcode := false
	resp := &atproto.ServerDescribeServer_Output{
		InviteCodeRequired:   &invcode,
		AvailableUserDomains: []string{},
		Links:                &atproto.ServerDescribeServer_Links{},
	}
	return c.JSON(http.StatusOK, resp)
}

// HandleSubscribeRepos opens and manages a websocket connection for subscribing to repo events
func (s *Server) HandleSubscribeRepos(c echo.Context) error {
	s.Logger.Infof("new repo subscription from %s\n", c.Request().RemoteAddr)
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

	ident := c.Request().RemoteAddr + "-" + c.Request().UserAgent()

	evts, cancel, err := s.Events.Subscribe(ctx, ident, func(evt *events.XRPCStreamEvent) bool {
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
