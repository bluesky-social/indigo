package bgs

import (
	"context"
	"encoding/json"
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
	comatproto "github.com/bluesky-social/indigo/api/atproto"
	"github.com/bluesky-social/indigo/blobs"
	"github.com/bluesky-social/indigo/carstore"
	"github.com/bluesky-social/indigo/events"
	"github.com/bluesky-social/indigo/indexer"
	lexutil "github.com/bluesky-social/indigo/lex/util"
	"github.com/bluesky-social/indigo/models"
	"github.com/bluesky-social/indigo/plc"
	"github.com/bluesky-social/indigo/repomgr"
	"github.com/bluesky-social/indigo/util"
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

	blobs blobs.BlobStore

	crawlOnly bool

	// TODO: at some point we will want to lock specific DIDs, this lock as is
	// is overly broad, but i dont expect it to be a bottleneck for now
	extUserLk sync.Mutex

	repoman *repomgr.RepoManager
}

func NewBGS(db *gorm.DB, ix *indexer.Indexer, repoman *repomgr.RepoManager, evtman *events.EventManager, didr plc.DidResolver, blobs blobs.BlobStore, ssl bool) (*BGS, error) {
	db.AutoMigrate(User{})
	db.AutoMigrate(models.PDS{})

	bgs := &BGS{
		Index: ix,
		db:    db,

		repoman: repoman,
		events:  evtman,
		didr:    didr,
		blobs:   blobs,
	}

	ix.CreateExternalUser = bgs.createExternalUser
	bgs.slurper = NewSlurper(db, bgs.handleFedEvent, ssl)

	if err := bgs.slurper.RestartAll(); err != nil {
		return nil, err
	}

	return bgs, nil
}

func (bgs *BGS) StartDebug(listen string) error {
	http.HandleFunc("/repodbg/user", func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()
		did := r.FormValue("did")

		u, err := bgs.Index.LookupUserByDid(ctx, did)
		if err != nil {
			http.Error(w, err.Error(), 400)
			return
		}

		root, err := bgs.repoman.GetRepoRoot(ctx, u.Uid)
		if err != nil {
			http.Error(w, err.Error(), 400)
			return
		}

		out := map[string]any{
			"root":      root.String(),
			"actorInfo": u,
		}

		if r.FormValue("carstore") != "" {
			stat, err := bgs.repoman.CarStore().Stat(ctx, u.Uid)
			if err != nil {
				http.Error(w, err.Error(), 400)
				return
			}
			out["carstore"] = stat
		}

		json.NewEncoder(w).Encode(out)
	})
	http.HandleFunc("/repodbg/blocks", func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()
		did := r.FormValue("did")
		c := r.FormValue("cid")

		bcid, err := cid.Decode(c)
		if err != nil {
			http.Error(w, err.Error(), 400)
			return
		}

		cs := bgs.repoman.CarStore()

		u, err := bgs.Index.LookupUserByDid(ctx, did)
		if err != nil {
			http.Error(w, err.Error(), 400)
			return
		}

		bs, err := cs.ReadOnlySession(u.Uid)
		if err != nil {
			http.Error(w, err.Error(), 400)
			return
		}

		blk, err := bs.Get(ctx, bcid)
		if err != nil {
			http.Error(w, err.Error(), 400)
			return
		}

		w.WriteHeader(200)
		w.Write(blk.RawData())

	})
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
		log.Warnf("HANDLER ERROR: (%s) %s", ctx.Path(), err)
		ctx.Response().WriteHeader(500)
	}

	// TODO: this API is temporary until we formalize what we want here

	e.GET("/xrpc/com.atproto.sync.subscribeRepos", bgs.EventsHandler)

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
	conn, err := websocket.Upgrade(c.Response(), c.Request(), c.Response().Header(), 1<<10, 1<<10)
	if err != nil {
		return fmt.Errorf("upgrading websocket: %w", err)
	}

	evts, cancel, err := bgs.events.Subscribe(ctx, func(evt *events.XRPCStreamEvent) bool { return true }, since)
	if err != nil {
		return err
	}
	defer cancel()

	header := events.EventHeader{Op: events.EvtKindMessage}
	for {
		select {
		case evt := <-evts:
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
	fmt.Println("HANDLE BGS FED EVENT", env.RepoHandle != nil, env.RepoCommit != nil)
	ctx, span := otel.Tracer("bgs").Start(ctx, "handleFedEvent")
	defer span.End()

	switch {
	case env.RepoCommit != nil:
		evt := env.RepoCommit
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

		// TODO: if the user is already in the 'slow' path, we shouldnt even bother trying to fast path this event

		if err := bgs.repoman.HandleExternalUserEvent(ctx, host.ID, u.ID, u.Did, (*cid.Cid)(evt.Prev), evt.Blocks); err != nil {
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
			var blobStrs []string
			for _, b := range evt.Blobs {
				blobStrs = append(blobStrs, b.String())
			}
			if err := bgs.syncUserBlobs(ctx, host, u.ID, blobStrs); err != nil {
				return err
			}
		}

		return nil
	case env.RepoHandle != nil:
		fmt.Println("handling handle update in bgs!!!!!")

		// TODO: ignoring the data in the message and just going out to the DID doc
		if _, err := bgs.createExternalUser(ctx, env.RepoHandle.Did); err != nil {
			return err
		}

		return nil
	case env.RepoMigrate != nil:
		if _, err := bgs.createExternalUser(ctx, env.RepoMigrate.Did); err != nil {
			return err
		}

		return nil
	default:
		return fmt.Errorf("invalid fed event")
	}
}

func (s *BGS) syncUserBlobs(ctx context.Context, pds *models.PDS, user bsutil.Uid, blobs []string) error {
	if s.blobs == nil {
		log.Infof("blob syncing disabled")
		return nil
	}

	did, err := s.Index.DidForUser(ctx, user)
	if err != nil {
		return err
	}

	for _, b := range blobs {
		c := models.ClientForPds(pds)
		blob, err := atproto.SyncGetBlob(ctx, c, b, did)
		if err != nil {
			return fmt.Errorf("fetching blob (%s, %s): %w", did, b, err)
		}

		if err := s.blobs.PutBlob(ctx, b, did, blob); err != nil {
			return fmt.Errorf("storing blob (%s, %s): %w", did, b, err)
		}
	}

	return nil
}

// TODO: rename? This also updates users, and 'external' is an old phrasing
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
		// TODO: the case of handling a new user on a new PDS probably requires more thought
		cfg, err := atproto.ServerDescribeServer(ctx, c)
		if err != nil {
			// TODO: failing this shouldnt halt our indexing
			return nil, fmt.Errorf("failed to check unrecognized pds: %w", err)
		}

		// since handles can be anything, checking against this list doesnt matter...
		_ = cfg

		// TODO: could check other things, a valid response is good enough for now
		peering.Host = durl.Host
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

	res, err := atproto.IdentityResolveHandle(ctx, c, handle)
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
		log.Infow("lost the race to create a new user", "did", did, "handle", handle)
		if exu.PDS != peering.ID {
			// User is now on a different PDS, update
			if err := s.db.Model(User{}).Where("id = ?", exu.ID).Update("pds", peering.ID).Error; err != nil {
				return nil, fmt.Errorf("failed to update users pds: %w", err)
			}

		}

		if exu.Handle != handle {
			// Users handle has changed, update
			if err := s.db.Model(User{}).Where("id = ?", exu.ID).Update("handle", peering.ID).Error; err != nil {
				return nil, fmt.Errorf("failed to update users handle: %w", err)
			}

			fmt.Println("HANDLE UPDATE DETECTED")
			if err := s.events.AddEvent(ctx, &events.XRPCStreamEvent{
				RepoHandle: &comatproto.SyncSubscribeRepos_Handle{
					Did:    exu.Did,
					Handle: handle,
					Time:   time.Now().Format(util.ISO8601),
				},
			}); err != nil {
				// TODO: should we really error here? I'm leaning towards no
				return nil, fmt.Errorf("failed to push handle update event: %s", err)
			}
		}
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
		Type:        "",
		PDS:         peering.ID,
	}
	if err := s.db.Create(subj).Error; err != nil {
		return nil, err
	}

	return subj, nil
}
