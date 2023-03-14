package bgs

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	_ "net/http/pprof"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"contrib.go.opencensus.io/exporter/prometheus"
	atproto "github.com/bluesky-social/indigo/api/atproto"
	"github.com/bluesky-social/indigo/carstore"
	"github.com/bluesky-social/indigo/events"
	"github.com/bluesky-social/indigo/indexer"
	lexutil "github.com/bluesky-social/indigo/lex/util"
	"github.com/bluesky-social/indigo/models"
	"github.com/bluesky-social/indigo/plc"
	"github.com/bluesky-social/indigo/repomgr"
	bsutil "github.com/bluesky-social/indigo/util"
	"github.com/bluesky-social/indigo/xrpc"
	"github.com/gorilla/websocket"
	"github.com/ipfs/go-cid"
	logging "github.com/ipfs/go-log"
	"github.com/labstack/echo/v4"
	"github.com/labstack/echo/v4/middleware"
	promclient "github.com/prometheus/client_golang/prometheus"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"gorm.io/gorm"
)

var log = logging.Logger("bgs")

type BGS struct {
	Index   *indexer.Indexer
	db      *gorm.DB
	slurper *Slurper
	events  *events.EventManager
	didr    plc.DidResolver

	crawlOnly bool

	// TODO: at some point we will want to lock specific DIDs, this lock as is
	// is overly broad, but i dont expect it to be a bottleneck for now
	extUserLk sync.Mutex

	repoman *repomgr.RepoManager
}

func NewBGS(db *gorm.DB, ix *indexer.Indexer, repoman *repomgr.RepoManager, evtman *events.EventManager, didr plc.DidResolver, ssl bool) (*BGS, error) {
	db.AutoMigrate(User{})
	db.AutoMigrate(models.PDS{})

	bgs := &BGS{
		Index: ix,
		db:    db,

		repoman: repoman,
		events:  evtman,
		didr:    didr,
	}

	ix.CreateExternalUser = bgs.createExternalUser
	bgs.slurper = NewSlurper(db, bgs.handleFedEvent, ssl)

	if err := bgs.slurper.RestartAll(); err != nil {
		return nil, err
	}

	return bgs, nil
}

func (bgs *BGS) StartDebug(listen string) error {
	http.Handle("/prometheus", prometheusHandler())

	return http.ListenAndServe(listen, nil)
}

func (bgs *BGS) Start(listen string) error {
	e := echo.New()
	e.HideBanner = true

	e.Use(middleware.LoggerWithConfig(middleware.LoggerConfig{
		Format: "method=${method}, uri=${uri}, status=${status} latency=${latency_human}\n",
	}))

	e.HTTPErrorHandler = func(err error, ctx echo.Context) {
		log.Errorf("HANDLER ERROR: (%s) %s", ctx.Path(), err)
		ctx.Response().WriteHeader(500)
	}

	// TODO: this API is temporary until we formalize what we want here

	e.GET("/xrpc/com.atproto.sync.subscribeAllRepos", bgs.EventsHandler)

	e.GET("/xrpc/com.atproto.sync.getCheckout", bgs.HandleComAtprotoSyncGetCheckout)
	e.GET("/xrpc/com.atproto.sync.getCommitPath", bgs.HandleComAtprotoSyncGetCommitPath)
	e.GET("/xrpc/com.atproto.sync.getHead", bgs.HandleComAtprotoSyncGetHead)
	e.GET("/xrpc/com.atproto.sync.getRecord", bgs.HandleComAtprotoSyncGetRecord)
	e.GET("/xrpc/com.atproto.sync.getRepo", bgs.HandleComAtprotoSyncGetRepo)
	e.GET("/xrpc/com.atproto.sync.getBlocks", bgs.HandleComAtprotoSyncGetBlocks)
	e.GET("/xrpc/com.atproto.sync.requestCrawl", bgs.HandleComAtprotoSyncRequestCrawl)
	e.GET("/xrpc/com.atproto.sync.notifyOfUpdate", bgs.HandleComAtprotoSyncNotifyOfUpdate)
	e.GET("/xrpc/_health", bgs.HandleHealthCheck)

	return e.Start(listen)
}

type HealthStatus struct {
	Status  string `json:"status"`
	Message string `json:"msg,omitempty"`
}

func (bgs *BGS) HandleHealthCheck(c echo.Context) error {
	if err := bgs.db.Exec("SELECT 1").Error; err != nil {
		log.Errorf("healthcheck can't connect to database: %v", err)
		return c.JSON(500, HealthStatus{Status: "error", Message: "can't connect to database"})
	} else {
		return c.JSON(200, HealthStatus{Status: "ok"})
	}
}

type User struct {
	ID        bsutil.Uid `gorm:"primarykey"`
	CreatedAt time.Time
	UpdatedAt time.Time
	DeletedAt gorm.DeletedAt `gorm:"index"`
	Handle    string         `gorm:"uniqueIndex"`
	Did       string         `gorm:"uniqueIndex"`
	PDS       uint
}

type addTargetBody struct {
	Host string `json:"host"`
}

func (bgs *BGS) EventsHandler(c echo.Context) error {
	var since *int64
	if sinceVal := c.QueryParam("cursor"); sinceVal != "" {
		sval, err := strconv.ParseInt(sinceVal, 10, 64)
		if err != nil {
			return err
		}
		since = &sval
	}

	ctx := c.Request().Context()

	// TODO: authhhh
	conn, err := websocket.Upgrade(c.Response().Writer, c.Request(), c.Response().Header(), 1<<10, 1<<10)
	if err != nil {
		return fmt.Errorf("upgrading websocket: %w", err)
	}

	evts, cancel, err := bgs.events.Subscribe(ctx, func(evt *events.XRPCStreamEvent) bool { return true }, since)
	if err != nil {
		return err
	}
	defer cancel()

	header := events.EventHeader{Op: events.EvtKindRepoAppend}
	for {
		select {
		case evt := <-evts:
			wc, err := conn.NextWriter(websocket.BinaryMessage)
			if err != nil {
				return err
			}

			var obj lexutil.CBOR

			switch {
			case evt.RepoAppend != nil:
				header.Op = events.EvtKindRepoAppend
				obj = evt.RepoAppend
			case evt.Error != nil:
				header.Op = events.EvtKindErrorFrame
				obj = evt.Error
			case evt.Info != nil:
				header.Op = events.EvtKindInfoFrame
				obj = evt.Info
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
		case <-ctx.Done():
			return nil
		}
	}
}

func prometheusHandler() http.Handler {
	// Prometheus globals are exposed as interfaces, but the prometheus
	// OpenCensus exporter expects a concrete *Registry. The concrete type of
	// the globals are actually *Registry, so we downcast them, staying
	// defensive in case things change under the hood.
	registry, ok := promclient.DefaultRegisterer.(*promclient.Registry)
	if !ok {
		log.Warnf("failed to export default prometheus registry; some metrics will be unavailable; unexpected type: %T", promclient.DefaultRegisterer)
	}
	exporter, err := prometheus.NewExporter(prometheus.Options{
		Registry:  registry,
		Namespace: "bigsky",
	})
	if err != nil {
		log.Errorf("could not create the prometheus stats exporter: %v", err)
	}

	return exporter
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

func (bgs *BGS) handleFedEvent(ctx context.Context, host *models.PDS, env *events.XRPCStreamEvent) error {
	ctx, span := otel.Tracer("bgs").Start(ctx, "handleFedEvent")
	defer span.End()

	switch {
	case env.RepoAppend != nil:
		evt := env.RepoAppend
		log.Infof("bgs got repo append event %d from %q: %s\n", evt.Seq, host.Host, evt.Repo)
		u, err := bgs.lookupUserByDid(ctx, evt.Repo)
		if err != nil {
			if !errors.Is(err, gorm.ErrRecordNotFound) {
				return fmt.Errorf("looking up event user: %w", err)
			}

			subj, err := bgs.createExternalUser(ctx, evt.Repo)
			if err != nil {
				return fmt.Errorf("fed event create external user: %w", err)
			}

			u = new(User)
			u.ID = subj.Uid
			u.Did = evt.Repo
		}

		var prevcid *cid.Cid
		if evt.Prev != nil {
			c, err := cid.Decode(*evt.Prev)
			if err != nil {
				return fmt.Errorf("invalid value for prev cid in event: %w", err)
			}
			prevcid = &c
		}

		// TODO: if the user is already in the 'slow' path, we shouldnt even bother trying to fast path this event

		if err := bgs.repoman.HandleExternalUserEvent(ctx, host.ID, u.ID, u.Did, prevcid, evt.Blocks); err != nil {
			log.Warnw("failed handling event", "err", err, "host", host.Host, "seq", evt.Seq)
			if !errors.Is(err, carstore.ErrRepoBaseMismatch) {
				return fmt.Errorf("handle user event failed: %w", err)
			}

			ai, err := bgs.Index.LookupUser(ctx, u.ID)
			if err != nil {
				return err
			}

			span.SetAttributes(attribute.Bool("catchup_queue", true))

			return bgs.Index.Crawler.AddToCatchupQueue(ctx, host, ai, evt)
		}

		// sync blobs
		if len(evt.Blobs) > 0 {
			if err := bgs.syncUserBlobs(ctx, host.ID, u.ID, evt.Blobs); err != nil {
				return err
			}
		}

		return nil
	default:
		return fmt.Errorf("invalid fed event")
	}
}

func (s *BGS) syncUserBlobs(ctx context.Context, pds uint, user bsutil.Uid, blobs []string) error {
	log.Warnf("not handling blob syncing yet")
	return nil
}

func (s *BGS) createExternalUser(ctx context.Context, did string) (*models.ActorInfo, error) {
	ctx, span := otel.Tracer("bgs").Start(ctx, "createExternalUser")
	defer span.End()

	log.Infof("create external user: %s", did)
	doc, err := s.didr.GetDocument(ctx, did)
	if err != nil {
		return nil, fmt.Errorf("could not locate DID document for followed user (%s): %w", did, err)
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
	var peering models.PDS
	if err := s.db.Find(&peering, "host = ?", durl.Host).Error; err != nil {
		log.Error("failed to find pds", durl.Host)
		return nil, err
	}

	c := &xrpc.Client{Host: durl.String()}

	if peering.ID == 0 {
		pdsdid, err := atproto.HandleResolve(ctx, c, "")
		if err != nil {
			// TODO: failing this shouldnt halt our indexing
			return nil, fmt.Errorf("failed to get accounts config for unrecognized pds: %w", err)
		}

		// TODO: could check other things, a valid response is good enough for now
		peering.Host = durl.Host
		peering.Did = pdsdid.Did
		peering.SSL = (durl.Scheme == "https")

		if err := s.db.Create(&peering).Error; err != nil {
			return nil, err
		}
	}

	if peering.ID == 0 {
		panic("somehow failed to create a pds entry?")
	}

	if len(doc.AlsoKnownAs) == 0 {
		return nil, fmt.Errorf("user has no 'known as' field in their DID document")
	}

	hurl, err := url.Parse(doc.AlsoKnownAs[0])
	if err != nil {
		return nil, err
	}

	handle := hurl.Host

	res, err := atproto.HandleResolve(ctx, c, handle)
	if err != nil {
		return nil, fmt.Errorf("failed to resolve users claimed handle (%q) on pds: %w", handle, err)
	}

	if res.Did != did {
		return nil, fmt.Errorf("claimed handle did not match servers response (%s != %s)", res.Did, did)
	}

	s.extUserLk.Lock()
	defer s.extUserLk.Unlock()

	exu, err := s.Index.LookupUserByDid(ctx, did)
	if err == nil {
		log.Infof("lost the race to create a new user: %s", did)
		return exu, nil
	}

	if !errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, err
	}

	// TODO: request this users info from their server to fill out our data...
	u := User{
		Handle: handle,
		Did:    did,
		PDS:    peering.ID,
	}

	if err := s.db.Create(&u).Error; err != nil {
		// some debugging...
		return nil, fmt.Errorf("failed to create other pds user: %w", err)
	}

	// okay cool, its a user on a server we are peered with
	// lets make a local record of that user for the future
	subj := &models.ActorInfo{
		Uid:         u.ID,
		Handle:      handle,
		DisplayName: "", //*profile.DisplayName,
		Did:         did,
		DeclRefCid:  "", // profile.Declaration.Cid,
		Type:        "",
		PDS:         peering.ID,
	}
	if err := s.db.Create(subj).Error; err != nil {
		return nil, err
	}

	return subj, nil
}
