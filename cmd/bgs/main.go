package main

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"strings"

	"github.com/gorilla/websocket"
	logging "github.com/ipfs/go-log"
	"github.com/labstack/echo/v4"
	"github.com/labstack/echo/v4/middleware"
	"github.com/urfave/cli/v2"
	"github.com/whyrusleeping/gosky/api"
	bsky "github.com/whyrusleeping/gosky/api/bsky"
	"github.com/whyrusleeping/gosky/carstore"
	cliutil "github.com/whyrusleeping/gosky/cmd/gosky/util"
	"github.com/whyrusleeping/gosky/events"
	"github.com/whyrusleeping/gosky/indexer"
	"github.com/whyrusleeping/gosky/notifs"
	"github.com/whyrusleeping/gosky/plc"
	"github.com/whyrusleeping/gosky/repomgr"
	"github.com/whyrusleeping/gosky/types"
	"github.com/whyrusleeping/gosky/xrpc"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/exporters/jaeger"
	"go.opentelemetry.io/otel/sdk/resource"
	tracesdk "go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.4.0"
	"gorm.io/gorm"
	"gorm.io/plugin/opentelemetry/tracing"
)

var log = logging.Logger("bgs")

func init() {
	logging.SetAllLoggers(logging.LevelDebug)
}

func main() {
	app := cli.NewApp()

	app.Flags = []cli.Flag{
		&cli.BoolFlag{
			Name: "jaeger",
		},
		&cli.StringFlag{
			Name:  "db",
			Value: "sqlite=bgs.db",
		},
		&cli.StringFlag{
			Name:  "carstoredb",
			Value: "sqlite=carstore.db",
		},
		&cli.StringFlag{
			Name:  "carstore",
			Value: "bgscarstore",
		},
		&cli.BoolFlag{
			Name: "dbtracing",
		},
		&cli.StringFlag{
			Name:  "plc",
			Usage: "hostname of the plc server",
			Value: "https://plc.directory",
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
					semconv.ServiceNameKey.String("bgs"),
					attribute.String("environment", "test"),
					attribute.Int64("ID", 1),
				)),
			)

			otel.SetTracerProvider(tp)
		}

		dbstr := cctx.String("db")

		db, err := cliutil.SetupDatabase(dbstr)
		if err != nil {
			return err
		}

		db.AutoMigrate(User{})
		db.AutoMigrate(PDS{})

		if cctx.Bool("dbtracing") {
			if err := db.Use(tracing.NewPlugin()); err != nil {
				return err
			}
		}

		cardb, err := cliutil.SetupDatabase(cctx.String("carstoredb"))
		if err != nil {
			return err
		}

		csdir := cctx.String("carstore")
		cstore, err := carstore.NewCarStore(cardb, csdir)
		if err != nil {
			return err
		}

		repoman := repomgr.NewRepoManager(db, cstore)

		evtman := events.NewEventManager()

		go evtman.Run()

		// not necessary to generate notifications, should probably make the
		// indexer just take optional callbacks for notification stuff
		notifman := notifs.NewNotificationManager(db, repoman.GetRecord)

		didr := &api.PLCServer{Host: cctx.String("plc")}

		ix, err := indexer.NewIndexer(db, notifman, evtman, didr)
		if err != nil {
			return err
		}

		repoman.SetEventHandler(func(ctx context.Context, evt *repomgr.RepoEvent) {
			if err := ix.HandleRepoEvent(ctx, evt); err != nil {
				log.Errorw("failed to handle repo event", "err", err)
			}
		})

		bgs := &BGS{
			index: ix,
			db:    db,

			repoman: repoman,
			events:  evtman,
			didr:    didr,
		}
		bgs.slurper = NewSlurper(db, bgs.handleFedEvent)

		return bgs.Start(":2470")
	}

	app.RunAndExitOnError()
}

type BGS struct {
	index   *indexer.Indexer
	db      *gorm.DB
	slurper *Slurper
	events  *events.EventManager
	didr    plc.PLCClient

	repoman *repomgr.RepoManager
}

func (bgs *BGS) Start(listen string) error {
	e := echo.New()

	e.Use(middleware.LoggerWithConfig(middleware.LoggerConfig{
		Format: "method=${method}, uri=${uri}, status=${status} latency=${latency_human}\n",
	}))

	e.HTTPErrorHandler = func(err error, ctx echo.Context) {
		fmt.Printf("HANDLER ERROR: (%s) %s\n", ctx.Path(), err)
		ctx.Response().WriteHeader(500)
	}

	// TODO: this API is temporary until we formalize what we want here
	e.POST("/add-target", bgs.handleAddTarget)

	e.GET("/events", bgs.EventsHandler)

	return e.Start(listen)
}

type PDS struct {
	gorm.Model

	Host string
}

type User struct {
	gorm.Model
	Handle string `gorm:"uniqueIndex"`
	Did    string `gorm:"uniqueIndex"`
	PDS    uint
}

type addTargetBody struct {
	Host string
}

// the ding-dong api
func (bgs *BGS) handleAddTarget(c echo.Context) error {
	var body addTargetBody
	if err := c.Bind(&body); err != nil {
		return err
	}

	return bgs.slurper.SubscribeToPds(c.Request().Context(), body.Host)
}

func (bgs *BGS) EventsHandler(c echo.Context) error {
	// TODO: authhhh
	conn, err := websocket.Upgrade(c.Response().Writer, c.Request(), c.Response().Header(), 1<<10, 1<<10)
	if err != nil {
		return err
	}

	evts, cancel, err := bgs.events.Subscribe(func(evt *events.Event) bool { return true })
	if err != nil {
		return err
	}
	defer cancel()

	for evt := range evts {
		if err := conn.WriteJSON(evt); err != nil {
			return err
		}
	}

	return nil
}

func (bgs *BGS) lookupUserByDid(ctx context.Context, did string) (*User, error) {
	var u User
	if err := bgs.db.Find(&u, "did = ?", did).Error; err != nil {
		return nil, err
	}

	if u.ID == 0 {
		return nil, gorm.ErrRecordNotFound
	}

	return &u, nil
}

func (bgs *BGS) handleFedEvent(ctx context.Context, host *PDS, evt *events.Event) error {
	log.Infof("got fed event from %q: %s\n", host.Host, evt.Kind)
	switch evt.Kind {
	case events.EvtKindRepoChange:
		u, err := bgs.lookupUserByDid(ctx, evt.Repo)
		if err != nil {
			if !errors.Is(err, gorm.ErrRecordNotFound) {
				return fmt.Errorf("looking up event user: %w", err)
			}

			subj, err := bgs.createExternalUser(ctx, evt.Repo)
			if err != nil {
				return err
			}

			u = new(User)
			u.ID = subj.Uid
		}

		return bgs.repoman.HandleExternalUserEvent(ctx, host.ID, u.ID, evt.RepoOps, evt.CarSlice)
	default:
		return fmt.Errorf("unrecognized fed event kind: %q", evt.Kind)
	}
	return nil
}

func (s *BGS) createExternalUser(ctx context.Context, did string) (*types.ActorInfo, error) {
	doc, err := s.didr.GetDocument(ctx, did)
	if err != nil {
		return nil, fmt.Errorf("could not locate DID document for followed user: %s", err)
	}

	if len(doc.Service) == 0 {
		return nil, fmt.Errorf("external followed user %s had no services in did document", did)
	}

	svc := doc.Service[0]
	durl, err := url.Parse(svc.ServiceEndpoint)
	if err != nil {
		return nil, err
	}

	if strings.HasPrefix(durl.Host, "localhost:") {
		durl.Scheme = "http"
	}

	// TODO: the PDS's DID should also be in the service, we could use that to look up?
	var peering PDS
	if err := s.db.First(&peering, "host = ?", durl.Host).Error; err != nil {
		return nil, err
	}

	var handle string
	if len(doc.AlsoKnownAs) > 0 {
		hurl, err := url.Parse(doc.AlsoKnownAs[0])
		if err != nil {
			return nil, err
		}

		handle = hurl.Host
	}

	c := &xrpc.Client{Host: durl.String()}
	profile, err := bsky.ActorGetProfile(ctx, c, did)
	if err != nil {
		return nil, err
	}

	if handle != profile.Handle {
		return nil, fmt.Errorf("mismatch in handle between did document and pds profile (%s != %s)", handle, profile.Handle)
	}

	// TODO: request this users info from their server to fill out our data...
	u := User{
		Handle: handle,
		Did:    did,
		PDS:    peering.ID,
	}

	if err := s.db.Create(&u).Error; err != nil {
		return nil, fmt.Errorf("failed to create other pds user: %w", err)
	}

	// okay cool, its a user on a server we are peered with
	// lets make a local record of that user for the future
	subj := &types.ActorInfo{
		Uid:         u.ID,
		Handle:      handle,
		DisplayName: *profile.DisplayName,
		Did:         did,
		DeclRefCid:  profile.Declaration.Cid,
		Type:        "",
		PDS:         peering.ID,
	}
	if err := s.db.Create(subj).Error; err != nil {
		return nil, err
	}

	return subj, nil
}
