package main

import (
	"context"
	"crypto/rand"
	"fmt"
	"log"
	"log/slog"
	"net"
	"os"
	"os/signal"
	"path/filepath"
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
	petname "github.com/dustinkirkland/golang-petname"
	"github.com/icrowley/fake"
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
	_ "go.uber.org/automaxprocs"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"

	"github.com/carlmjohnson/versioninfo"
	cbg "github.com/whyrusleeping/cbor-gen"
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
	Logger       *slog.Logger
	EventControl chan string
	MultibaseKey string
	RepoManager  *repomgr.RepoManager

	// Event Loop Parameters
	TotalDesiredEvents int
	MaxEventsPerSecond int

	PlaybackFile string
}

func main() {
	ctx := context.Background()
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	app := cli.App{
		Name:    "supercollider",
		Usage:   "atproto event noise-maker for Relay load testing",
		Version: versioninfo.Short(),
	}

	app.Flags = []cli.Flag{
		&cli.StringFlag{
			Name:    "hostname",
			Usage:   "hostname of this server (forward *.hostname DNS records to this server)",
			Value:   "supercollider.jazco.io",
			EnvVars: []string{"SUPERCOLLIDER_HOST"},
		},
		&cli.BoolFlag{
			Name:    "use-ssl",
			Usage:   "listen on port 443 and use SSL (needs to be run as root and have external DNS setup)",
			Value:   false,
			EnvVars: []string{"SUPERCOLLIDER_USE_SSL"},
		},
		&cli.IntFlag{
			Name:    "port",
			Usage:   "port for the HTTP(S) server to listen on (defaults to 80 if not using SSL, 443 if using SSL)",
			EnvVars: []string{"SUPERCOLLIDER_PORT"},
		},

		&cli.StringFlag{
			Name:    "key-file",
			Usage:   "file to store the private key used to sign events",
			Value:   "key.raw",
			EnvVars: []string{"KEY_FILE"},
		},
	}

	app.Commands = []*cli.Command{
		{
			Name:   "reload",
			Usage:  "reload events from a file and write them to an output file",
			Action: Reload,
			Flags: append([]cli.Flag{
				&cli.IntFlag{
					Name:    "num-users",
					Usage:   "number of fake users to produce events for",
					Value:   100,
					EnvVars: []string{"NUM_USERS"},
				},
				&cli.IntFlag{
					Name:    "total-events",
					Usage:   "total number of events to generate",
					Value:   1_000_000,
					EnvVars: []string{"TOTAL_EVENTS"},
				},
				&cli.StringFlag{
					Name:    "output-file",
					Usage:   "output file for the generated events",
					Value:   "events_out.cbor",
					EnvVars: []string{"OUTPUT_FILE"},
				},
			}, app.Flags...),
		},
		{
			Name:   "fire",
			Usage:  "fire events from a file over a websocket",
			Action: Fire,
			Flags: append([]cli.Flag{
				&cli.IntFlag{
					Name:    "events-per-second",
					Usage:   "maximum number of events to generate per second",
					Value:   300,
					EnvVars: []string{"EVENTS_PER_SECOND"},
				},
				&cli.StringFlag{
					Name:    "input-file",
					Usage:   "input file for the generated events (if set, will read events from this file instead of generating them)",
					Value:   "events_in.cbor",
					EnvVars: []string{"INPUT_FILE"},
				},
			}, app.Flags...),
		},
	}

	err := app.Run(os.Args)
	if err != nil {
		log.Fatal(err)
	}
}

func Reload(cctx *cli.Context) error {
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

	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{
		Level: slog.LevelInfo,
	}))
	defer func() {
		logger.Info("main function teardown")
	}()

	logger = logger.With("source", "supercollider_main")

	logger.Info("Starting Supercollider in Reload Mode")
	logger.Info(fmt.Sprintf("Generating %d total events and writing them to %s",
		cctx.Int("total-events"), cctx.String("output-file")))

	em := events.NewEventManager(events.NewYoloPersister())

	// Try to read the key from disk
	keyBytes, err := os.ReadFile(cctx.String("key-file"))
	if err != nil {
		logger.Warn("failed to read key from disk, creating new key", "err", err.Error())
	}

	var privkey *godid.PrivKey
	if len(keyBytes) == 0 {
		privkey, err = godid.GeneratePrivKey(rand.Reader, godid.KeyTypeSecp256k1)
		if err != nil {
			log.Fatalf("failed to generate privkey: %+v\n", err)
		}
		rawKey, err := privkey.RawBytes()
		if err != nil {
			log.Fatalf("failed to serialize privkey: %+v\n", err)
		}
		err = os.WriteFile(cctx.String("key-file"), rawKey, 0644)
		if err != nil {
			log.Fatalf("failed to write privkey to disk: %+v\n", err)
		}
	} else {
		privkey, err = godid.PrivKeyFromRawBytes(godid.KeyTypeSecp256k1, keyBytes)
		if err != nil {
			log.Fatalf("failed to parse privkey from disk: %+v\n", err)
		}
	}

	// Configure the repomanager and keypair for our fake accounts
	repoman, privkey, err := initSpeedyRepoMan(privkey)
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
		Logger:    logger,
		EnableSSL: cctx.Bool("use-ssl"),
		Host:      cctx.String("hostname"),

		RepoManager:  repoman,
		MultibaseKey: *vMethod.PublicKeyMultibase,
		Dids:         dids,

		Events:             em,
		TotalDesiredEvents: cctx.Int("total-events"),
	}

	repoman.SetEventHandler(s.HandleRepoEvent, false)

	// HTTP Server setup and Middleware Plumbing
	e := echo.New()
	e.AutoTLSManager.Cache = autocert.DirCache("/var/www/.cache")
	pprof.Register(e)
	e.Use(middleware.LoggerWithConfig(middleware.LoggerConfig{
		Format: "method=${method}, ip=${remote_ip}, uri=${uri}, status=${status} latency=${latency_human} (ua=${user_agent})\n",
	}))

	e.GET("/", func(c echo.Context) error {
		return c.HTML(http.StatusOK, `<h1>Supercollider is reloading...</h1>`)
	})
	e.GET("/metrics", echo.WrapHandler(promhttp.Handler()))

	port := cctx.Int("port")
	if port == 0 {
		if cctx.Bool("use-ssl") {
			port = 443
		} else {
			port = 80
		}
	}

	wg := sync.WaitGroup{}
	wg.Add(1)
	// Start a loop to subscribe to events and write them to a file
	go func() {
		defer wg.Done()
		outFile := cctx.String("output-file")
		f, err := os.OpenFile(outFile, os.O_CREATE|os.O_WRONLY, 0644)
		if err != nil {
			log.Fatalf("failed to open output file: %+v\n", err)
		}
		defer f.Close()
		since := int64(0)

		evts, cancel, err := s.Events.Subscribe(ctx, "supercollider_file", func(evt *events.XRPCStreamEvent) bool {
			return true
		}, &since)
		if err != nil {
			log.Fatalf("failed to subscribe to events: %+v\n", err)
		}
		defer cancel()

		logger.Info("writing events", "path", outFile)

		header := events.EventHeader{Op: events.EvtKindMessage}
		for {
			select {
			case <-ctx.Done():
				logger.Info("shutting down file writer")
				err = f.Sync()
				if err != nil {
					logger.Error("failed to sync file", "err", err)
				}
				logger.Info("file writer shutdown complete")
				return
			case evt := <-evts:
				if evt.Error != nil {
					logger.Error("error in event stream", "err", evt.Error)
					continue
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
					logger.Error("unrecognized event kind")
					continue
				}

				if err := header.MarshalCBOR(f); err != nil {
					logger.Error("failed to write header", "err", err)
				}

				if err := obj.MarshalCBOR(f); err != nil {
					logger.Error("failed to write event", "err", err)
				}
			}
		}
	}()

	// Start the event generation loop
	go func() {
		time.Sleep(time.Second * 5)
		s.EventGenerationLoop(ctx, cancel)
	}()

	listenAddress := fmt.Sprintf(":%d", port)
	go func() {
		if cctx.Bool("use-ssl") {
			err = e.StartAutoTLS(listenAddress)
		} else {
			err = e.Start(listenAddress)
		}
		if err != nil {
			logger.Error("failed to start server", "err", err)
		}
	}()
	<-ctx.Done()
	logger.Info("shutting down server...")
	wg.Wait()
	logger.Info("server shutdown complete")
	return nil
}

func Fire(cctx *cli.Context) error {
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

	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{
		Level: slog.LevelInfo,
	}))

	defer func() {
		logger.Info("main function teardown")
	}()

	logger = logger.With("source", "supercollider_main")
	logger.Info("Starting Supercollider in Fire Mode")

	// Try to read the key from disk
	keyBytes, err := os.ReadFile(cctx.String("key-file"))
	if err != nil {
		logger.Warn("failed to read key from disk, creating new key", "err", err.Error())
	}

	var privkey *godid.PrivKey
	if len(keyBytes) == 0 {
		privkey, err = godid.GeneratePrivKey(rand.Reader, godid.KeyTypeSecp256k1)
		if err != nil {
			log.Fatalf("failed to generate privkey: %+v\n", err)
		}
		rawKey, err := privkey.RawBytes()
		if err != nil {
			log.Fatalf("failed to serialize privkey: %+v\n", err)
		}
		err = os.WriteFile(cctx.String("key-file"), rawKey, 0644)
		if err != nil {
			log.Fatalf("failed to write privkey to disk: %+v\n", err)
		}
	} else {
		privkey, err = godid.PrivKeyFromRawBytes(godid.KeyTypeSecp256k1, keyBytes)
		if err != nil {
			log.Fatalf("failed to parse privkey from disk: %+v\n", err)
		}
	}

	vMethod, err := godid.VerificationMethodFromKey(privkey.Public())
	if err != nil {
		log.Fatalf("failed to generate verification method: %+v\n", err)
	}

	// Instantiate Server
	s := &Server{
		Logger:             logger,
		EnableSSL:          cctx.Bool("use-ssl"),
		Host:               cctx.String("hostname"),
		MultibaseKey:       *vMethod.PublicKeyMultibase,
		MaxEventsPerSecond: cctx.Int("events-per-second"),
		PlaybackFile:       cctx.String("input-file"),
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
				logger.Error("Failed to write http error", "err", err2)
			}
		default:
			sendHeader := true
			if ctx.Path() == "/xrpc/com.atproto.sync.subscribeRepos" {
				sendHeader = false
			}

			logger.Warn("HANDLER ERROR", "path", ctx.Path(), "err", err)

			if sendHeader {
				ctx.Response().WriteHeader(500)
			}
		}
	}

	e.GET("/", func(c echo.Context) error {
		return c.HTML(http.StatusOK, `<h1>Supercollider is firing...</h1>`)
	})

	e.GET("/.well-known/did.json", s.HandleWellKnownDid)
	e.GET("/.well-known/atproto-did", s.HandleAtprotoDid)
	e.GET("/xrpc/com.atproto.server.describeServer", s.DescribeServerHandler)
	e.GET("/xrpc/com.atproto.sync.subscribeRepos", s.HandleSubscribeRepos)
	e.GET("/metrics", echo.WrapHandler(promhttp.Handler()))

	port := cctx.Int("port")
	if port == 0 {
		if cctx.Bool("use-ssl") {
			port = 443
		} else {
			port = 80
		}
	}

	listenAddress := fmt.Sprintf(":%d", port)
	go func() {
		if cctx.Bool("use-ssl") {
			err = e.StartAutoTLS(listenAddress)
		} else {
			err = e.Start(listenAddress)
		}
		if err != nil {
			logger.Error("failed to start server", "err", err)
		}
	}()
	<-ctx.Done()
	logger.Info("shutting down server")
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

// Stand up a Repo Manager with a Web DID Resolver
func initSpeedyRepoMan(key *godid.PrivKey) (*repomgr.RepoManager, *godid.PrivKey, error) {
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

	cs, err := carstore.NewCarStore(cardb, []string{cspath})
	if err != nil {
		return nil, nil, err
	}

	mr := did.NewMultiResolver()
	mr.AddHandler("web", &did.WebResolver{
		Insecure: true,
	})

	cachedidr := plc.NewCachingDidResolver(mr, time.Minute*5, 1000)

	kmgr := indexer.NewKeyManager(cachedidr, key)

	repoman := repomgr.NewRepoManager(cs, kmgr)

	return repoman, key, nil
}

// HandleRepoEvent is the callback for the RepoManager
func (s *Server) HandleRepoEvent(ctx context.Context, evt *repomgr.RepoEvent) {
	outops := make([]*comatproto.SyncSubscribeRepos_RepoOp, 0, len(evt.Ops))
	for _, op := range evt.Ops {
		link := (*lexutil.LexLink)(op.RecCid)
		outops = append(outops, &comatproto.SyncSubscribeRepos_RepoOp{
			Path:   op.Collection + "/" + op.Rkey,
			Action: string(op.Kind),
			Cid:    link,
		})
	}

	if err := s.Events.AddEvent(ctx, &events.XRPCStreamEvent{
		RepoCommit: &comatproto.SyncSubscribeRepos_Commit{
			Repo:   s.Dids[evt.User-1],
			Prev:   (*lexutil.LexLink)(evt.OldRoot),
			Blocks: evt.RepoSlice,
			Commit: lexutil.LexLink(evt.NewRoot),
			Time:   time.Now().Format(util.ISO8601),
			Ops:    outops,
			TooBig: false,
		},
		PrivUid: evt.User,
	}); err != nil {
		s.Logger.Error("failed to add event", "err", err)
	}
}

// EventGenerationLoop is the main loop for generating events
func (s *Server) EventGenerationLoop(ctx context.Context, cancel context.CancelFunc) {
	defer cancel()
	s.Logger.Info(fmt.Sprintf("starting event generation for %d events", s.TotalDesiredEvents))

	s.Logger.Info(fmt.Sprintf("initializing %d fake users", len(s.Dids)))
	for i, did := range s.Dids {
		uid := models.Uid(i + 1)
		if err := s.RepoManager.InitNewActor(ctx, uid, strings.TrimPrefix(did, "did:web:"), did, "catdog", "", ""); err != nil {
			log.Fatalf("failed to init actor: %+v\n", err)
		}
	}

	s.Logger.Info("generating events", "count", s.TotalDesiredEvents)

	for i := 0; i < s.TotalDesiredEvents; i++ {
		text := fake.SentencesN(3)
		// Trim to 300 chars
		if len(text) > 300 {
			text = text[:300]
		}
		_, _, err := s.RepoManager.CreateRecord(ctx, models.Uid(i%len(s.Dids)+1), "app.bsky.feed.post", &bsky.FeedPost{
			CreatedAt: time.Now().Format(util.ISO8601),
			Text:      text,
		})
		if err != nil {
			s.Logger.Error("failed to create record", "err", err)
		} else {
			eventsGeneratedCounter.Inc()
		}
		select {
		case <-ctx.Done():
			s.Logger.Info("shutting down event generation loop on context done")
			return
		default:
		}
	}

	s.Logger.Info("event generation complete, shutting down")
	return
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
	s.Logger.Info("new repo subscription", "remote", c.Request().RemoteAddr)
	conn, err := websocket.Upgrade(c.Response().Writer, c.Request(), c.Response().Header(), 1<<10, 1<<10)
	if err != nil {
		return err
	}
	defer conn.Close()

	ctx := c.Request().Context()

	limiter := rate.NewLimiter(rate.Limit(s.MaxEventsPerSecond), 10)

	f, err := os.Open(s.PlaybackFile)
	if err != nil {
		s.Logger.Error("failed to open playback file", "err", err)
		return err
	}
	defer f.Close()

	// Set a ping handler
	conn.SetPingHandler(func(message string) error {
		err := conn.WriteControl(websocket.PongMessage, []byte(message), time.Now().Add(time.Second*60))
		if err == websocket.ErrCloseSent {
			return nil
		} else if e, ok := err.(net.Error); ok && e.Temporary() {
			return nil
		}
		return err
	})

	// Start a goroutine to read messages from the client and discard them.
	go func() {
		for {
			_, _, err := conn.ReadMessage()
			if err != nil {
				return
			}
		}
	}()

	header := cbg.Deferred{}
	obj := cbg.Deferred{}
	for {
		wc, err := conn.NextWriter(websocket.BinaryMessage)
		if err != nil {
			return err
		}

		limiter.Wait(ctx)

		if err := header.UnmarshalCBOR(f); err != nil {
			return fmt.Errorf("failed to read header: %w", err)
		}
		if err := obj.UnmarshalCBOR(f); err != nil {
			return fmt.Errorf("failed to read event: %w", err)
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
}
