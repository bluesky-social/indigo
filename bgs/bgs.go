package bgs

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	_ "net/http/pprof"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"contrib.go.opencensus.io/exporter/prometheus"
	"github.com/bluesky-social/indigo/api"
	atproto "github.com/bluesky-social/indigo/api/atproto"
	comatproto "github.com/bluesky-social/indigo/api/atproto"
	"github.com/bluesky-social/indigo/blobs"
	"github.com/bluesky-social/indigo/carstore"
	"github.com/bluesky-social/indigo/did"
	"github.com/bluesky-social/indigo/events"
	"github.com/bluesky-social/indigo/indexer"
	lexutil "github.com/bluesky-social/indigo/lex/util"
	"github.com/bluesky-social/indigo/models"
	"github.com/bluesky-social/indigo/repomgr"
	"github.com/bluesky-social/indigo/xrpc"
	"golang.org/x/sync/semaphore"
	"golang.org/x/time/rate"

	"github.com/gorilla/websocket"
	"github.com/ipfs/go-cid"
	ipld "github.com/ipfs/go-ipld-format"
	logging "github.com/ipfs/go-log"
	"github.com/labstack/echo/v4"
	"github.com/labstack/echo/v4/middleware"
	promclient "github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	dto "github.com/prometheus/client_model/go"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"gorm.io/gorm"
)

var log = logging.Logger("bgs")

// serverListenerBootTimeout is how long to wait for the requested server socket
// to become available for use. This is an arbitrary timeout that should be safe
// on any platform, but there's no great way to weave this timeout without
// adding another parameter to the (at time of writing) long signature of
// NewServer.
const serverListenerBootTimeout = 5 * time.Second

type BGS struct {
	Index       *indexer.Indexer
	db          *gorm.DB
	slurper     *Slurper
	events      *events.EventManager
	didr        did.Resolver
	repoFetcher *indexer.RepoFetcher

	blobs blobs.BlobStore
	hr    api.HandleResolver

	// TODO: work on doing away with this flag in favor of more pluggable
	// pieces that abstract the need for explicit ssl checks
	ssl bool

	crawlOnly bool

	// TODO: at some point we will want to lock specific DIDs, this lock as is
	// is overly broad, but i dont expect it to be a bottleneck for now
	extUserLk sync.Mutex

	repoman *repomgr.RepoManager

	// Management of Socket Consumers
	consumersLk    sync.RWMutex
	nextConsumerID uint64
	consumers      map[uint64]*SocketConsumer

	// Management of Resyncs
	pdsResyncsLk sync.RWMutex
	pdsResyncs   map[uint]*PDSResync

	// Management of Compaction
	compactor *Compactor
}

type PDSResync struct {
	PDS              models.PDS `json:"pds"`
	NumRepoPages     int        `json:"numRepoPages"`
	NumRepos         int        `json:"numRepos"`
	NumReposChecked  int        `json:"numReposChecked"`
	NumReposToResync int        `json:"numReposToResync"`
	Status           string     `json:"status"`
	StatusChangedAt  time.Time  `json:"statusChangedAt"`
}

type SocketConsumer struct {
	UserAgent   string
	RemoteAddr  string
	ConnectedAt time.Time
	EventsSent  promclient.Counter
}

func NewBGS(db *gorm.DB, ix *indexer.Indexer, repoman *repomgr.RepoManager, evtman *events.EventManager, didr did.Resolver, blobs blobs.BlobStore, rf *indexer.RepoFetcher, hr api.HandleResolver, ssl bool) (*BGS, error) {
	db.AutoMigrate(User{})
	db.AutoMigrate(AuthToken{})
	db.AutoMigrate(models.PDS{})
	db.AutoMigrate(models.DomainBan{})

	bgs := &BGS{
		Index:       ix,
		db:          db,
		repoFetcher: rf,

		hr:      hr,
		repoman: repoman,
		events:  evtman,
		didr:    didr,
		blobs:   blobs,
		ssl:     ssl,

		consumersLk: sync.RWMutex{},
		consumers:   make(map[uint64]*SocketConsumer),

		pdsResyncs: make(map[uint]*PDSResync),
	}

	ix.CreateExternalUser = bgs.createExternalUser
	slOpts := DefaultSlurperOptions()
	slOpts.SSL = ssl
	s, err := NewSlurper(db, bgs.handleFedEvent, slOpts)
	if err != nil {
		return nil, err
	}

	bgs.slurper = s

	if err := bgs.slurper.RestartAll(); err != nil {
		return nil, err
	}

	compactor := NewCompactor(nil)
	compactor.Start(bgs)
	bgs.compactor = compactor

	return bgs, nil
}

func (bgs *BGS) StartMetrics(listen string) error {
	http.Handle("/metrics", promhttp.Handler())
	return http.ListenAndServe(listen, nil)
}

// Disabled for now, maybe reimplement behind admin auth later
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
	http.HandleFunc("/repodbg/crawl", func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()
		did := r.FormValue("did")

		act, err := bgs.Index.GetUserOrMissing(ctx, did)
		if err != nil {
			w.WriteHeader(500)
			log.Errorf("failed to get user: %s", err)
			return
		}

		if err := bgs.Index.Crawler.Crawl(ctx, act); err != nil {
			w.WriteHeader(500)
			log.Errorf("failed to add user to crawler: %s", err)
			return
		}
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

	return http.ListenAndServe(listen, nil)
}

func (bgs *BGS) Start(addr string) error {
	var lc net.ListenConfig
	ctx, cancel := context.WithTimeout(context.Background(), serverListenerBootTimeout)
	defer cancel()

	li, err := lc.Listen(ctx, "tcp", addr)
	if err != nil {
		return err
	}
	return bgs.StartWithListener(li)
}

func (bgs *BGS) StartWithListener(listen net.Listener) error {
	e := echo.New()
	e.HideBanner = true

	e.Use(middleware.CORSWithConfig(middleware.CORSConfig{
		AllowOrigins: []string{"http://localhost:*", "https://bgs.bsky-sandbox.dev"},
		AllowHeaders: []string{echo.HeaderOrigin, echo.HeaderContentType, echo.HeaderAccept, echo.HeaderAuthorization},
	}))

	if !bgs.ssl {
		e.Use(middleware.LoggerWithConfig(middleware.LoggerConfig{
			Format: "method=${method}, uri=${uri}, status=${status} latency=${latency_human}\n",
		}))
	} else {
		e.Use(middleware.LoggerWithConfig(middleware.DefaultLoggerConfig))
	}

	// React uses a virtual router, so we need to serve the index.html for all
	// routes that aren't otherwise handled or in the /assets directory.
	e.File("/dash", "/public/index.html")
	e.File("/dash/*", "/public/index.html")
	e.Static("/assets", "/public/assets")

	e.Use(MetricsMiddleware)

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

			if strings.HasPrefix(ctx.Path(), "/admin/") {
				ctx.JSON(500, map[string]any{
					"error": err.Error(),
				})
				return
			}

			if sendHeader {
				ctx.Response().WriteHeader(500)
			}
		}
	}

	// TODO: this API is temporary until we formalize what we want here

	e.GET("/xrpc/com.atproto.sync.subscribeRepos", bgs.EventsHandler)
	e.GET("/xrpc/com.atproto.sync.getRecord", bgs.HandleComAtprotoSyncGetRecord)
	e.GET("/xrpc/com.atproto.sync.getRepo", bgs.HandleComAtprotoSyncGetRepo)
	e.GET("/xrpc/com.atproto.sync.getBlocks", bgs.HandleComAtprotoSyncGetBlocks)
	e.GET("/xrpc/com.atproto.sync.requestCrawl", bgs.HandleComAtprotoSyncRequestCrawl)
	e.POST("/xrpc/com.atproto.sync.requestCrawl", bgs.HandleComAtprotoSyncRequestCrawl)
	e.GET("/xrpc/com.atproto.sync.listRepos", bgs.HandleComAtprotoSyncListRepos)
	e.GET("/xrpc/com.atproto.sync.getLatestCommit", bgs.HandleComAtprotoSyncGetLatestCommit)
	e.GET("/xrpc/com.atproto.sync.notifyOfUpdate", bgs.HandleComAtprotoSyncNotifyOfUpdate)
	e.GET("/xrpc/_health", bgs.HandleHealthCheck)

	admin := e.Group("/admin", bgs.checkAdminAuth)

	// Slurper-related Admin API
	admin.GET("/subs/getUpstreamConns", bgs.handleAdminGetUpstreamConns)
	admin.GET("/subs/getEnabled", bgs.handleAdminGetSubsEnabled)
	admin.POST("/subs/setEnabled", bgs.handleAdminSetSubsEnabled)
	admin.POST("/subs/killUpstream", bgs.handleAdminKillUpstreamConn)

	// Domain-related Admin API
	admin.GET("/subs/listDomainBans", bgs.handleAdminListDomainBans)
	admin.POST("/subs/banDomain", bgs.handleAdminBanDomain)
	admin.POST("/subs/unbanDomain", bgs.handleAdminUnbanDomain)

	// Repo-related Admin API
	admin.POST("/repo/takeDown", bgs.handleAdminTakeDownRepo)
	admin.POST("/repo/reverseTakedown", bgs.handleAdminReverseTakedown)
	admin.POST("/repo/compact", bgs.handleAdminCompactRepo)
	admin.POST("/repo/compactAll", bgs.handleAdminCompactAllRepos)
	admin.POST("/repo/reset", bgs.handleAdminResetRepo)

	// PDS-related Admin API
	admin.GET("/pds/list", bgs.handleListPDSs)
	admin.POST("/pds/resync", bgs.handleAdminPostResyncPDS)
	admin.GET("/pds/resync", bgs.handleAdminGetResyncPDS)
	admin.POST("/pds/changeIngestRateLimit", bgs.handleAdminChangePDSRateLimit)
	admin.POST("/pds/changeCrawlRateLimit", bgs.handleAdminChangePDSCrawlLimit)
	admin.POST("/pds/block", bgs.handleBlockPDS)
	admin.POST("/pds/unblock", bgs.handleUnblockPDS)
	admin.POST("/pds/addTrustedDomain", bgs.handleAdminAddTrustedDomain)

	// Consumer-related Admin API
	admin.GET("/consumers/list", bgs.handleAdminListConsumers)

	// In order to support booting on random ports in tests, we need to tell the
	// Echo instance it's already got a port, and then use its StartServer
	// method to re-use that listener.
	e.Listener = listen
	srv := &http.Server{}
	return e.StartServer(srv)
}

func (bgs *BGS) Shutdown() []error {
	errs := bgs.slurper.Shutdown()

	if err := bgs.events.Shutdown(context.TODO()); err != nil {
		errs = append(errs, err)
	}

	bgs.compactor.Shutdown()

	return errs
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

type AuthToken struct {
	gorm.Model
	Token string `gorm:"index"`
}

func (bgs *BGS) lookupAdminToken(tok string) (bool, error) {
	var at AuthToken
	if err := bgs.db.Find(&at, "token = ?", tok).Error; err != nil {
		return false, err
	}

	if at.ID == 0 {
		return false, nil
	}

	return true, nil
}

func (bgs *BGS) CreateAdminToken(tok string) error {
	exists, err := bgs.lookupAdminToken(tok)
	if err != nil {
		return err
	}

	if exists {
		return nil
	}

	return bgs.db.Create(&AuthToken{
		Token: tok,
	}).Error
}

func (bgs *BGS) checkAdminAuth(next echo.HandlerFunc) echo.HandlerFunc {
	return func(e echo.Context) error {
		ctx, span := otel.Tracer("bgs").Start(e.Request().Context(), "checkAdminAuth")
		defer span.End()

		e.SetRequest(e.Request().WithContext(ctx))

		authheader := e.Request().Header.Get("Authorization")
		pref := "Bearer "
		if !strings.HasPrefix(authheader, pref) {
			return echo.ErrForbidden
		}

		token := authheader[len(pref):]

		exists, err := bgs.lookupAdminToken(token)
		if err != nil {
			return err
		}

		if !exists {
			return echo.ErrForbidden
		}

		return next(e)
	}
}

type User struct {
	ID          models.Uid `gorm:"primarykey;index:idx_user_id_active,where:taken_down = false AND tombstoned = false"`
	CreatedAt   time.Time
	UpdatedAt   time.Time
	DeletedAt   gorm.DeletedAt `gorm:"index"`
	Handle      sql.NullString `gorm:"index"`
	Did         string         `gorm:"uniqueIndex"`
	PDS         uint
	ValidHandle bool `gorm:"default:true"`

	// TakenDown is set to true if the user in question has been taken down.
	// A user in this state will have all future events related to it dropped
	// and no data about this user will be served.
	TakenDown  bool
	Tombstoned bool
}

type addTargetBody struct {
	Host string `json:"host"`
}

func (bgs *BGS) registerConsumer(c *SocketConsumer) uint64 {
	bgs.consumersLk.Lock()
	defer bgs.consumersLk.Unlock()

	id := bgs.nextConsumerID
	bgs.nextConsumerID++

	bgs.consumers[id] = c

	return id
}

func (bgs *BGS) cleanupConsumer(id uint64) {
	bgs.consumersLk.Lock()
	defer bgs.consumersLk.Unlock()

	c := bgs.consumers[id]

	var m = &dto.Metric{}
	if err := c.EventsSent.Write(m); err != nil {
		log.Errorf("failed to get sent counter: %s", err)
	}

	log.Infow("consumer disconnected",
		"consumer_id", id,
		"remote_addr", c.RemoteAddr,
		"user_agent", c.UserAgent,
		"events_sent", m.Counter.GetValue())

	delete(bgs.consumers, id)
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

	ctx, cancel := context.WithCancel(c.Request().Context())
	defer cancel()

	// TODO: authhhh
	conn, err := websocket.Upgrade(c.Response(), c.Request(), c.Response().Header(), 10<<10, 10<<10)
	if err != nil {
		return fmt.Errorf("upgrading websocket: %w", err)
	}

	defer conn.Close()

	lastWriteLk := sync.Mutex{}
	lastWrite := time.Now()

	// Start a goroutine to ping the client every 30 seconds to check if it's
	// still alive. If the client doesn't respond to a ping within 5 seconds,
	// we'll close the connection and teardown the consumer.
	go func() {
		ticker := time.NewTicker(30 * time.Second)
		defer ticker.Stop()

		for {
			select {
			case <-ticker.C:
				lastWriteLk.Lock()
				lw := lastWrite
				lastWriteLk.Unlock()

				if time.Since(lw) < 30*time.Second {
					continue
				}

				if err := conn.WriteControl(websocket.PingMessage, []byte{}, time.Now().Add(5*time.Second)); err != nil {
					log.Warnf("failed to ping client: %s", err)
					cancel()
					return
				}
			case <-ctx.Done():
				return
			}
		}
	}()

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
				log.Warnf("failed to read message from client: %s", err)
				cancel()
				return
			}
		}
	}()

	ident := c.RealIP() + "-" + c.Request().UserAgent()

	evts, cleanup, err := bgs.events.Subscribe(ctx, ident, func(evt *events.XRPCStreamEvent) bool { return true }, since)
	if err != nil {
		return err
	}
	defer cleanup()

	// Keep track of the consumer for metrics and admin endpoints
	consumer := SocketConsumer{
		RemoteAddr:  c.RealIP(),
		UserAgent:   c.Request().UserAgent(),
		ConnectedAt: time.Now(),
	}
	sentCounter := eventsSentCounter.WithLabelValues(consumer.RemoteAddr, consumer.UserAgent)
	consumer.EventsSent = sentCounter

	consumerID := bgs.registerConsumer(&consumer)
	defer bgs.cleanupConsumer(consumerID)

	logger := log.With(
		"consumer_id", consumerID,
		"remote_addr", consumer.RemoteAddr,
		"user_agent", consumer.UserAgent,
	)

	logger.Infow("new consumer", "cursor", since)

	header := events.EventHeader{Op: events.EvtKindMessage}
	for {
		select {
		case evt, ok := <-evts:
			if !ok {
				logger.Error("event stream closed unexpectedly")
				return nil
			}

			wc, err := conn.NextWriter(websocket.BinaryMessage)
			if err != nil {
				logger.Errorf("failed to get next writer: %s", err)
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
				logger.Warnf("failed to flush-close our event write: %s", err)
				return nil
			}

			lastWriteLk.Lock()
			lastWrite = time.Now()
			lastWriteLk.Unlock()
			sentCounter.Inc()
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

// domainIsBanned checks if the given host is banned, starting with the host
// itself, then checking every parent domain up to the tld
func (s *BGS) domainIsBanned(ctx context.Context, host string) (bool, error) {
	// ignore ports when checking for ban status
	hostport := strings.Split(host, ":")

	segments := strings.Split(hostport[0], ".")

	// TODO: use normalize method once that merges
	var cleaned []string
	for _, s := range segments {
		if s == "" {
			continue
		}
		s = strings.ToLower(s)

		cleaned = append(cleaned, s)
	}
	segments = cleaned

	for i := 0; i < len(segments)-1; i++ {
		dchk := strings.Join(segments[i:], ".")
		found, err := s.findDomainBan(ctx, dchk)
		if err != nil {
			return false, err
		}

		if found {
			return true, nil
		}
	}
	return false, nil
}

func (s *BGS) findDomainBan(ctx context.Context, host string) (bool, error) {
	var db models.DomainBan
	if err := s.db.Find(&db, "domain = ?", host).Error; err != nil {
		return false, err
	}

	if db.ID == 0 {
		return false, nil
	}

	return true, nil
}

func (bgs *BGS) lookupUserByDid(ctx context.Context, did string) (*User, error) {
	ctx, span := otel.Tracer("bgs").Start(ctx, "lookupUserByDid")
	defer span.End()

	var u User
	if err := bgs.db.Find(&u, "did = ?", did).Error; err != nil {
		return nil, err
	}

	if u.ID == 0 {
		return nil, gorm.ErrRecordNotFound
	}

	return &u, nil
}

func (bgs *BGS) lookupUserByUID(ctx context.Context, uid models.Uid) (*User, error) {
	ctx, span := otel.Tracer("bgs").Start(ctx, "lookupUserByUID")
	defer span.End()

	var u User
	if err := bgs.db.Find(&u, "id = ?", uid).Error; err != nil {
		return nil, err
	}

	if u.ID == 0 {
		return nil, gorm.ErrRecordNotFound
	}

	return &u, nil
}

func stringLink(lnk *lexutil.LexLink) string {
	if lnk == nil {
		return "<nil>"
	}

	return lnk.String()
}

func (bgs *BGS) handleFedEvent(ctx context.Context, host *models.PDS, env *events.XRPCStreamEvent) error {
	ctx, span := otel.Tracer("bgs").Start(ctx, "handleFedEvent")
	defer span.End()

	start := time.Now()
	defer func() {
		eventsHandleDuration.WithLabelValues(host.Host).Observe(time.Since(start).Seconds())
	}()

	eventsReceivedCounter.WithLabelValues(host.Host).Add(1)

	switch {
	case env.RepoCommit != nil:
		repoCommitsReceivedCounter.WithLabelValues(host.Host).Add(1)
		evt := env.RepoCommit
		log.Debugw("bgs got repo append event", "seq", evt.Seq, "host", host.Host, "repo", evt.Repo)
		u, err := bgs.lookupUserByDid(ctx, evt.Repo)
		if err != nil {
			if !errors.Is(err, gorm.ErrRecordNotFound) {
				return fmt.Errorf("looking up event user: %w", err)
			}

			newUsersDiscovered.Inc()
			subj, err := bgs.createExternalUser(ctx, evt.Repo)
			if err != nil {
				return fmt.Errorf("fed event create external user: %w", err)
			}

			u = new(User)
			u.ID = subj.Uid
			u.Did = evt.Repo
		}

		if u.TakenDown {
			log.Debugw("dropping event from taken down user", "did", evt.Repo, "seq", evt.Seq, "host", host.Host)
			return nil
		}

		if evt.Rebase {
			return fmt.Errorf("rebase was true in event seq:%d,host:%s", evt.Seq, host.Host)
		}

		if host.ID != u.PDS && u.PDS != 0 {
			log.Warnw("received event for repo from different pds than expected", "repo", evt.Repo, "expPds", u.PDS, "gotPds", host.Host)
			// Flush any cached DID documents for this user
			bgs.didr.FlushCacheFor(env.RepoCommit.Repo)

			subj, err := bgs.createExternalUser(ctx, evt.Repo)
			if err != nil {
				return err
			}

			if subj.PDS != host.ID {
				return fmt.Errorf("event from non-authoritative pds")
			}
		}

		if u.Tombstoned {
			// we've checked the authority of the users PDS, so reinstate the account
			if err := bgs.db.Model(&User{}).Where("id = ?", u.ID).UpdateColumn("tombstoned", false).Error; err != nil {
				return fmt.Errorf("failed to un-tombstone a user: %w", err)
			}

			ai, err := bgs.Index.LookupUser(ctx, u.ID)
			if err != nil {
				return fmt.Errorf("failed to look up user (tombstone recover): %w", err)
			}

			// Now a simple re-crawl should suffice to bring the user back online
			return bgs.Index.Crawler.AddToCatchupQueue(ctx, host, ai, evt)
		}

		// skip the fast path for rebases or if the user is already in the slow path
		if bgs.Index.Crawler.RepoInSlowPath(ctx, u.ID) {
			rebasesCounter.WithLabelValues(host.Host).Add(1)
			ai, err := bgs.Index.LookupUser(ctx, u.ID)
			if err != nil {
				return fmt.Errorf("failed to look up user (slow path): %w", err)
			}

			// TODO: we currently do not handle events that get queued up
			// behind an already 'in progress' slow path event.
			// this is strictly less efficient than it could be, and while it
			// does 'work' (due to falling back to resyncing the repo), its
			// technically incorrect. Now that we have the parallel event
			// processor coming off of the pds stream, we should investigate
			// whether or not we even need this 'slow path' logic, as it makes
			// accounting for which events have been processed much harder
			return bgs.Index.Crawler.AddToCatchupQueue(ctx, host, ai, evt)
		}

		if err := bgs.repoman.HandleExternalUserEvent(ctx, host.ID, u.ID, u.Did, evt.Since, evt.Rev, evt.Blocks, evt.Ops); err != nil {
			log.Warnw("failed handling event", "err", err, "host", host.Host, "seq", evt.Seq, "repo", u.Did, "prev", stringLink(evt.Prev), "commit", evt.Commit.String())

			if errors.Is(err, carstore.ErrRepoBaseMismatch) || ipld.IsNotFound(err) {
				ai, lerr := bgs.Index.LookupUser(ctx, u.ID)
				if lerr != nil {
					return fmt.Errorf("failed to look up user %s (%d) (err case: %s): %w", u.Did, u.ID, err, lerr)
				}

				span.SetAttributes(attribute.Bool("catchup_queue", true))

				return bgs.Index.Crawler.AddToCatchupQueue(ctx, host, ai, evt)
			}

			return fmt.Errorf("handle user event failed: %w", err)
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
		log.Infow("bgs got repo handle event", "did", env.RepoHandle.Did, "handle", env.RepoHandle.Handle)
		// Flush any cached DID documents for this user
		bgs.didr.FlushCacheFor(env.RepoHandle.Did)

		// TODO: ignoring the data in the message and just going out to the DID doc
		act, err := bgs.createExternalUser(ctx, env.RepoHandle.Did)
		if err != nil {
			return err
		}

		if act.Handle.String != env.RepoHandle.Handle {
			log.Warnw("handle update did not update handle to asserted value", "did", env.RepoHandle.Did, "expected", env.RepoHandle.Handle, "actual", act.Handle)
		}

		// TODO: Update the ReposHandle event type to include "verified" or something

		// Broadcast the handle update to all consumers
		err = bgs.events.AddEvent(ctx, &events.XRPCStreamEvent{
			RepoHandle: &comatproto.SyncSubscribeRepos_Handle{
				Did:    env.RepoHandle.Did,
				Handle: env.RepoHandle.Handle,
				Time:   env.RepoHandle.Time,
			},
		})
		if err != nil {
			log.Errorw("failed to broadcast RepoHandle event", "error", err, "did", env.RepoHandle.Did, "handle", env.RepoHandle.Handle)
			return fmt.Errorf("failed to broadcast RepoHandle event: %w", err)
		}

		return nil
	case env.RepoMigrate != nil:
		if _, err := bgs.createExternalUser(ctx, env.RepoMigrate.Did); err != nil {
			return err
		}

		return nil
	case env.RepoTombstone != nil:
		if err := bgs.handleRepoTombstone(ctx, host, env.RepoTombstone); err != nil {
			return err
		}

		return nil
	default:
		return fmt.Errorf("invalid fed event")
	}
}

func (bgs *BGS) handleRepoTombstone(ctx context.Context, pds *models.PDS, evt *atproto.SyncSubscribeRepos_Tombstone) error {
	u, err := bgs.lookupUserByDid(ctx, evt.Did)
	if err != nil {
		return err
	}

	if u.PDS != pds.ID {
		return fmt.Errorf("unauthoritative tombstone event from %s for %s", pds.Host, evt.Did)
	}

	if err := bgs.db.Model(&User{}).Where("id = ?", u.ID).UpdateColumns(map[string]any{
		"tombstoned": true,
		"handle":     nil,
	}).Error; err != nil {
		return err
	}

	if err := bgs.db.Model(&models.ActorInfo{}).Where("uid = ?", u.ID).UpdateColumns(map[string]any{
		"handle": nil,
	}).Error; err != nil {
		return err
	}

	// delete data from carstore
	if err := bgs.repoman.TakeDownRepo(ctx, u.ID); err != nil {
		// don't let a failure here prevent us from propagating this event
		log.Errorf("failed to delete user data from carstore: %s", err)
	}

	return bgs.events.AddEvent(ctx, &events.XRPCStreamEvent{
		RepoTombstone: evt,
	})
}

func (s *BGS) syncUserBlobs(ctx context.Context, pds *models.PDS, user models.Uid, blobs []string) error {
	if s.blobs == nil {
		log.Debugf("blob syncing disabled")
		return nil
	}

	did, err := s.Index.DidForUser(ctx, user)
	if err != nil {
		return err
	}

	for _, b := range blobs {
		c := models.ClientForPds(pds)
		s.Index.ApplyPDSClientSettings(c)
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

	externalUserCreationAttempts.Inc()

	log.Debugf("create external user: %s", did)
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

	if peering.Blocked {
		return nil, fmt.Errorf("refusing to create user with blocked PDS")
	}

	ban, err := s.domainIsBanned(ctx, durl.Host)
	if err != nil {
		return nil, fmt.Errorf("failed to check pds ban status: %w", err)
	}

	if ban {
		return nil, fmt.Errorf("cannot create user on pds with banned domain")
	}

	c := &xrpc.Client{Host: durl.String()}
	s.Index.ApplyPDSClientSettings(c)

	if peering.ID == 0 {
		// TODO: the case of handling a new user on a new PDS probably requires more thought
		cfg, err := atproto.ServerDescribeServer(ctx, c)
		if err != nil {
			// TODO: failing this shouldn't halt our indexing
			return nil, fmt.Errorf("failed to check unrecognized pds: %w", err)
		}

		// since handles can be anything, checking against this list doesn't matter...
		_ = cfg

		// TODO: could check other things, a valid response is good enough for now
		peering.Host = durl.Host
		peering.SSL = (durl.Scheme == "https")

		if s.ssl && !peering.SSL {
			return nil, fmt.Errorf("did references non-ssl PDS, this is disallowed in prod: %q %q", did, svc.ServiceEndpoint)
		}

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

	log.Debugw("creating external user", "did", did, "handle", hurl.Host, "pds", peering.ID)

	handle := hurl.Host

	validHandle := true

	resdid, err := s.hr.ResolveHandleToDid(ctx, handle)
	if err != nil {
		log.Errorf("failed to resolve users claimed handle (%q) on pds: %s", handle, err)
		validHandle = false
	}

	if resdid != did {
		log.Errorf("claimed handle did not match servers response (%s != %s)", resdid, did)
		validHandle = false
	}

	s.extUserLk.Lock()
	defer s.extUserLk.Unlock()

	exu, err := s.Index.LookupUserByDid(ctx, did)
	if err == nil {
		log.Debugw("lost the race to create a new user", "did", did, "handle", handle, "existing_hand", exu.Handle)
		if exu.PDS != peering.ID {
			// User is now on a different PDS, update
			if err := s.db.Model(User{}).Where("id = ?", exu.Uid).Update("pds", peering.ID).Error; err != nil {
				return nil, fmt.Errorf("failed to update users pds: %w", err)
			}

			if err := s.db.Model(models.ActorInfo{}).Where("uid = ?", exu.Uid).Update("pds", peering.ID).Error; err != nil {
				return nil, fmt.Errorf("failed to update users pds on actorInfo: %w", err)
			}

			exu.PDS = peering.ID
		}

		if exu.Handle.String != handle {
			// Users handle has changed, update
			if err := s.db.Model(User{}).Where("id = ?", exu.Uid).Update("handle", handle).Error; err != nil {
				return nil, fmt.Errorf("failed to update users handle: %w", err)
			}

			// Update ActorInfos
			if err := s.db.Model(models.ActorInfo{}).Where("uid = ?", exu.Uid).Update("handle", handle).Error; err != nil {
				return nil, fmt.Errorf("failed to update actorInfos handle: %w", err)
			}

			exu.Handle = sql.NullString{String: handle, Valid: true}
		}
		return exu, nil
	}

	if !errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, err
	}

	// TODO: request this users info from their server to fill out our data...
	u := User{
		Did:         did,
		PDS:         peering.ID,
		ValidHandle: validHandle,
	}
	if validHandle {
		u.Handle = sql.NullString{String: handle, Valid: true}
	}

	if err := s.db.Create(&u).Error; err != nil {
		// If the new user's handle conflicts with an existing user,
		// since we just validated the handle for this user, we'll assume
		// the existing user no longer has control of the handle
		if errors.Is(err, gorm.ErrDuplicatedKey) {
			// Get the UID of the existing user
			var existingUser User
			if err := s.db.Find(&existingUser, "handle = ?", handle).Error; err != nil {
				return nil, fmt.Errorf("failed to find existing user: %w", err)
			}

			// Set the existing user's handle to NULL and set the valid_handle flag to false
			if err := s.db.Model(User{}).Where("id = ?", existingUser.ID).Update("handle", nil).Update("valid_handle", false).Error; err != nil {
				return nil, fmt.Errorf("failed to update outdated user's handle: %w", err)
			}

			// Do the same thing for the ActorInfo if it exists
			if err := s.db.Model(models.ActorInfo{}).Where("uid = ?", existingUser.ID).Update("handle", nil).Update("valid_handle", false).Error; err != nil {
				if !errors.Is(err, gorm.ErrRecordNotFound) {
					return nil, fmt.Errorf("failed to update outdated actorInfo's handle: %w", err)
				}
			}

			// Create the new user
			if err := s.db.Create(&u).Error; err != nil {
				return nil, fmt.Errorf("failed to create user after handle conflict: %w", err)
			}
		} else {
			return nil, fmt.Errorf("failed to create other pds user: %w", err)
		}
	}

	// okay cool, its a user on a server we are peered with
	// lets make a local record of that user for the future
	subj := &models.ActorInfo{
		Uid:         u.ID,
		DisplayName: "", //*profile.DisplayName,
		Did:         did,
		Type:        "",
		PDS:         peering.ID,
		ValidHandle: validHandle,
	}
	if validHandle {
		subj.Handle = sql.NullString{String: handle, Valid: true}
	}
	if err := s.db.Create(subj).Error; err != nil {
		return nil, err
	}

	return subj, nil
}

func (bgs *BGS) TakeDownRepo(ctx context.Context, did string) error {
	u, err := bgs.lookupUserByDid(ctx, did)
	if err != nil {
		return err
	}

	if err := bgs.db.Model(User{}).Where("id = ?", u.ID).Update("taken_down", true).Error; err != nil {
		return err
	}

	if err := bgs.repoman.TakeDownRepo(ctx, u.ID); err != nil {
		return err
	}

	if err := bgs.events.TakeDownRepo(ctx, u.ID); err != nil {
		return err
	}

	return nil
}

func (bgs *BGS) ReverseTakedown(ctx context.Context, did string) error {
	u, err := bgs.lookupUserByDid(ctx, did)
	if err != nil {
		return err
	}

	if err := bgs.db.Model(User{}).Where("id = ?", u.ID).Update("taken_down", false).Error; err != nil {
		return err
	}

	return nil
}

type revCheckResult struct {
	ai  *models.ActorInfo
	err error
}

func (bgs *BGS) LoadOrStoreResync(pds models.PDS) (PDSResync, bool) {
	bgs.pdsResyncsLk.Lock()
	defer bgs.pdsResyncsLk.Unlock()

	if r, ok := bgs.pdsResyncs[pds.ID]; ok && r != nil {
		return *r, true
	}

	r := PDSResync{
		PDS:             pds,
		Status:          "started",
		StatusChangedAt: time.Now(),
	}

	bgs.pdsResyncs[pds.ID] = &r

	return r, false
}

func (bgs *BGS) GetResync(pds models.PDS) (PDSResync, bool) {
	bgs.pdsResyncsLk.RLock()
	defer bgs.pdsResyncsLk.RUnlock()

	if r, ok := bgs.pdsResyncs[pds.ID]; ok {
		return *r, true
	}

	return PDSResync{}, false
}

func (bgs *BGS) UpdateResync(resync PDSResync) {
	bgs.pdsResyncsLk.Lock()
	defer bgs.pdsResyncsLk.Unlock()

	bgs.pdsResyncs[resync.PDS.ID] = &resync
}

func (bgs *BGS) SetResyncStatus(id uint, status string) PDSResync {
	bgs.pdsResyncsLk.Lock()
	defer bgs.pdsResyncsLk.Unlock()

	if r, ok := bgs.pdsResyncs[id]; ok {
		r.Status = status
		r.StatusChangedAt = time.Now()
	}

	return *bgs.pdsResyncs[id]
}

func (bgs *BGS) CompleteResync(resync PDSResync) {
	bgs.pdsResyncsLk.Lock()
	defer bgs.pdsResyncsLk.Unlock()

	delete(bgs.pdsResyncs, resync.PDS.ID)
}

func (bgs *BGS) ResyncPDS(ctx context.Context, pds models.PDS) error {
	ctx, span := otel.Tracer("bgs").Start(ctx, "ResyncPDS")
	defer span.End()
	log := log.With("pds", pds.Host, "source", "resync_pds")
	resync, found := bgs.LoadOrStoreResync(pds)
	if found {
		return fmt.Errorf("resync already in progress")
	}
	defer bgs.CompleteResync(resync)

	start := time.Now()

	log.Warn("starting PDS resync")

	host := "http://"
	if pds.SSL {
		host = "https://"
	}
	host += pds.Host

	xrpcc := xrpc.Client{Host: host}
	bgs.Index.ApplyPDSClientSettings(&xrpcc)

	limiter := rate.NewLimiter(rate.Limit(50), 1)
	cursor := ""
	limit := int64(500)

	repos := []comatproto.SyncListRepos_Repo{}

	pages := 0

	resync = bgs.SetResyncStatus(pds.ID, "listing repos")
	for {
		pages++
		if pages%10 == 0 {
			log.Warnw("fetching PDS page during resync", "pages", pages, "total_repos", len(repos))
			resync.NumRepoPages = pages
			resync.NumRepos = len(repos)
			bgs.UpdateResync(resync)
		}
		if err := limiter.Wait(ctx); err != nil {
			log.Errorw("failed to wait for rate limiter", "error", err)
			return fmt.Errorf("failed to wait for rate limiter: %w", err)
		}
		repoList, err := comatproto.SyncListRepos(ctx, &xrpcc, cursor, limit)
		if err != nil {
			log.Errorw("failed to list repos", "error", err)
			return fmt.Errorf("failed to list repos: %w", err)
		}

		for _, r := range repoList.Repos {
			if r != nil {
				repos = append(repos, *r)
			}
		}

		if repoList.Cursor == nil || *repoList.Cursor == "" {
			break
		}
		cursor = *repoList.Cursor
	}

	resync.NumRepoPages = pages
	resync.NumRepos = len(repos)
	bgs.UpdateResync(resync)

	repolistDone := time.Now()

	log.Warnw("listed all repos, checking roots", "num_repos", len(repos), "took", repolistDone.Sub(start))
	resync = bgs.SetResyncStatus(pds.ID, "checking revs")

	// Create a buffered channel for collecting results
	results := make(chan revCheckResult, len(repos))
	sem := semaphore.NewWeighted(40)

	// Check repo revs against our local copy and enqueue crawls for any that are out of date
	for _, r := range repos {
		go func(r comatproto.SyncListRepos_Repo) {
			if err := sem.Acquire(ctx, 1); err != nil {
				log.Errorw("failed to acquire semaphore", "error", err)
				results <- revCheckResult{err: err}
				return
			}
			defer sem.Release(1)

			log := log.With("did", r.Did, "remote_rev", r.Rev)
			// Fetches the user if we have it, otherwise automatically enqueues it for crawling
			ai, err := bgs.Index.GetUserOrMissing(ctx, r.Did)
			if err != nil {
				log.Errorw("failed to get user while resyncing PDS, we can't recrawl it", "error", err)
				results <- revCheckResult{err: err}
				return
			}

			rev, err := bgs.repoman.GetRepoRev(ctx, ai.Uid)
			if err != nil {
				log.Warnw("recrawling because we failed to get the local repo root", "err", err, "uid", ai.Uid)
				results <- revCheckResult{ai: ai}
				return
			}

			if rev == "" || rev < r.Rev {
				log.Warnw("recrawling because the repo rev from the PDS is newer than our local repo rev", "local_rev", rev)
				results <- revCheckResult{ai: ai}
				return
			}

			results <- revCheckResult{}
		}(r)
	}

	var numReposToResync int
	for i := 0; i < len(repos); i++ {
		res := <-results
		if res.err != nil {
			log.Errorw("failed to process repo during resync", "error", res.err)

		}
		if res.ai != nil {
			numReposToResync++
			err := bgs.Index.Crawler.Crawl(ctx, res.ai)
			if err != nil {
				log.Errorw("failed to enqueue crawl for repo during resync", "error", err, "uid", res.ai.Uid, "did", res.ai.Did)
			}
		}
		if i%100_000 == 0 {
			log.Warnw("checked revs during resync", "num_repos_checked", i, "num_repos_to_crawl", numReposToResync, "took", time.Now().Sub(resync.StatusChangedAt))
			resync.NumReposChecked = i
			resync.NumReposToResync = numReposToResync
			bgs.UpdateResync(resync)
		}
	}

	resync.NumReposChecked = len(repos)
	resync.NumReposToResync = numReposToResync
	bgs.UpdateResync(resync)

	log.Warnw("enqueued all crawls, exiting resync", "took", time.Now().Sub(start), "num_repos_to_crawl", numReposToResync)

	return nil
}
