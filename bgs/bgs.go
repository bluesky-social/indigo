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
	"github.com/bluesky-social/indigo/carstore"
	"github.com/bluesky-social/indigo/did"
	"github.com/bluesky-social/indigo/events"
	"github.com/bluesky-social/indigo/indexer"
	lexutil "github.com/bluesky-social/indigo/lex/util"
	"github.com/bluesky-social/indigo/models"
	"github.com/bluesky-social/indigo/repomgr"
	"github.com/bluesky-social/indigo/xrpc"
	lru "github.com/hashicorp/golang-lru/v2"
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
var tracer = otel.Tracer("bgs")

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

	hr api.HandleResolver

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

	// User cache
	userCache *lru.Cache[string, *User]
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

type BGSConfig struct {
	SSL                  bool
	CompactInterval      time.Duration
	DefaultRepoLimit     int64
	ConcurrencyPerPDS    int64
	MaxQueuePerPDS       int64
	NumCompactionWorkers int
}

func DefaultBGSConfig() *BGSConfig {
	return &BGSConfig{
		SSL:                  true,
		CompactInterval:      4 * time.Hour,
		DefaultRepoLimit:     100,
		ConcurrencyPerPDS:    100,
		MaxQueuePerPDS:       1_000,
		NumCompactionWorkers: 2,
	}
}

func NewBGS(db *gorm.DB, ix *indexer.Indexer, repoman *repomgr.RepoManager, evtman *events.EventManager, didr did.Resolver, rf *indexer.RepoFetcher, hr api.HandleResolver, config *BGSConfig) (*BGS, error) {

	if config == nil {
		config = DefaultBGSConfig()
	}
	db.AutoMigrate(User{})
	db.AutoMigrate(AuthToken{})
	db.AutoMigrate(models.PDS{})
	db.AutoMigrate(models.DomainBan{})

	uc, _ := lru.New[string, *User](1_000_000)

	bgs := &BGS{
		Index:       ix,
		db:          db,
		repoFetcher: rf,

		hr:      hr,
		repoman: repoman,
		events:  evtman,
		didr:    didr,
		ssl:     config.SSL,

		consumersLk: sync.RWMutex{},
		consumers:   make(map[uint64]*SocketConsumer),

		pdsResyncs: make(map[uint]*PDSResync),

		userCache: uc,
	}

	ix.CreateExternalUser = bgs.createExternalUser
	slOpts := DefaultSlurperOptions()
	slOpts.SSL = config.SSL
	slOpts.DefaultRepoLimit = config.DefaultRepoLimit
	slOpts.ConcurrencyPerPDS = config.ConcurrencyPerPDS
	slOpts.MaxQueuePerPDS = config.MaxQueuePerPDS
	s, err := NewSlurper(db, bgs.handleFedEvent, slOpts)
	if err != nil {
		return nil, err
	}

	bgs.slurper = s

	if err := bgs.slurper.RestartAll(); err != nil {
		return nil, err
	}

	cOpts := DefaultCompactorOptions()
	cOpts.NumWorkers = config.NumCompactionWorkers
	compactor := NewCompactor(cOpts)
	compactor.requeueInterval = config.CompactInterval
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
		AllowOrigins: []string{"*"},
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
	e.File("/dash", "public/index.html")
	e.File("/dash/*", "public/index.html")
	e.Static("/assets", "public/assets")

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
	e.GET("/_health", bgs.HandleHealthCheck)
	e.GET("/", bgs.HandleHomeMessage)

	admin := e.Group("/admin", bgs.checkAdminAuth)

	// Slurper-related Admin API
	admin.GET("/subs/getUpstreamConns", bgs.handleAdminGetUpstreamConns)
	admin.GET("/subs/getEnabled", bgs.handleAdminGetSubsEnabled)
	admin.GET("/subs/perDayLimit", bgs.handleAdminGetNewPDSPerDayRateLimit)
	admin.POST("/subs/setEnabled", bgs.handleAdminSetSubsEnabled)
	admin.POST("/subs/killUpstream", bgs.handleAdminKillUpstreamConn)
	admin.POST("/subs/setPerDayLimit", bgs.handleAdminSetNewPDSPerDayRateLimit)

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
	admin.POST("/repo/verify", bgs.handleAdminVerifyRepo)

	// PDS-related Admin API
	admin.POST("/pds/requestCrawl", bgs.handleAdminRequestCrawl)
	admin.GET("/pds/list", bgs.handleListPDSs)
	admin.POST("/pds/resync", bgs.handleAdminPostResyncPDS)
	admin.GET("/pds/resync", bgs.handleAdminGetResyncPDS)
	admin.POST("/pds/changeLimits", bgs.handleAdminChangePDSRateLimits)
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

var homeMessage string = `
d8888b. d888888b  d888b  .d8888. db   dD db    db
88  '8D   '88'   88' Y8b 88'  YP 88 ,8P' '8b  d8'
88oooY'    88    88      '8bo.   88,8P    '8bd8'
88~~~b.    88    88  ooo   'Y8b. 88'8b      88
88   8D   .88.   88. ~8~ db   8D 88 '88.    88
Y8888P' Y888888P  Y888P  '8888Y' YP   YD    YP

This is an atproto [https://atproto.com] relay instance, running the 'bigsky' codebase [https://github.com/bluesky-social/indigo]

The firehose WebSocket path is at:  /xrpc/com.atproto.sync.subscribeRepos
`

func (bgs *BGS) HandleHomeMessage(c echo.Context) error {
	return c.String(http.StatusOK, homeMessage)
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
		ctx, span := tracer.Start(e.Request().Context(), "checkAdminAuth")
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

	// UpstreamStatus is the state of the user as reported by the upstream PDS
	UpstreamStatus string `gorm:"index"`

	lk sync.Mutex
}

func (u *User) SetTakenDown(v bool) {
	u.lk.Lock()
	defer u.lk.Unlock()
	u.TakenDown = v
}

func (u *User) GetTakenDown() bool {
	u.lk.Lock()
	defer u.lk.Unlock()
	return u.TakenDown
}

func (u *User) SetTombstoned(v bool) {
	u.lk.Lock()
	defer u.lk.Unlock()
	u.Tombstoned = v
}

func (u *User) GetTombstoned() bool {
	u.lk.Lock()
	defer u.lk.Unlock()
	return u.Tombstoned
}

func (u *User) SetUpstreamStatus(v string) {
	u.lk.Lock()
	defer u.lk.Unlock()
	u.UpstreamStatus = v
}

func (u *User) GetUpstreamStatus() string {
	u.lk.Lock()
	defer u.lk.Unlock()
	return u.UpstreamStatus
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

			if evt.Preserialized != nil {
				_, err = wc.Write(evt.Preserialized)
			} else {
				err = evt.Serialize(wc)
			}
			if err != nil {
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
	ctx, span := tracer.Start(ctx, "lookupUserByDid")
	defer span.End()

	cu, ok := bgs.userCache.Get(did)
	if ok {
		return cu, nil
	}

	var u User
	if err := bgs.db.Find(&u, "did = ?", did).Error; err != nil {
		return nil, err
	}

	if u.ID == 0 {
		return nil, gorm.ErrRecordNotFound
	}

	bgs.userCache.Add(did, &u)

	return &u, nil
}

func (bgs *BGS) lookupUserByUID(ctx context.Context, uid models.Uid) (*User, error) {
	ctx, span := tracer.Start(ctx, "lookupUserByUID")
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
	ctx, span := tracer.Start(ctx, "handleFedEvent")
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
		log.Debugw("bgs got repo append event", "seq", evt.Seq, "pdsHost", host.Host, "repo", evt.Repo)

		s := time.Now()
		u, err := bgs.lookupUserByDid(ctx, evt.Repo)
		userLookupDuration.Observe(time.Since(s).Seconds())
		if err != nil {
			if !errors.Is(err, gorm.ErrRecordNotFound) {
				return fmt.Errorf("looking up event user: %w", err)
			}

			newUsersDiscovered.Inc()
			start := time.Now()
			subj, err := bgs.createExternalUser(ctx, evt.Repo)
			newUserDiscoveryDuration.Observe(time.Since(start).Seconds())
			if err != nil {
				return fmt.Errorf("fed event create external user: %w", err)
			}

			u = new(User)
			u.ID = subj.Uid
			u.Did = evt.Repo
		}

		ustatus := u.GetUpstreamStatus()
		span.SetAttributes(attribute.String("upstream_status", ustatus))

		if u.GetTakenDown() || ustatus == events.AccountStatusTakendown {
			span.SetAttributes(attribute.Bool("taken_down_by_relay_admin", u.GetTakenDown()))
			log.Debugw("dropping commit event from taken down user", "did", evt.Repo, "seq", evt.Seq, "pdsHost", host.Host)
			return nil
		}

		if ustatus == events.AccountStatusSuspended {
			log.Debugw("dropping commit event from suspended user", "did", evt.Repo, "seq", evt.Seq, "pdsHost", host.Host)
			return nil
		}

		if ustatus == events.AccountStatusDeactivated {
			log.Debugw("dropping commit event from deactivated user", "did", evt.Repo, "seq", evt.Seq, "pdsHost", host.Host)
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

		if u.GetTombstoned() {
			span.SetAttributes(attribute.Bool("tombstoned", true))
			// we've checked the authority of the users PDS, so reinstate the account
			if err := bgs.db.Model(&User{}).Where("id = ?", u.ID).UpdateColumn("tombstoned", false).Error; err != nil {
				return fmt.Errorf("failed to un-tombstone a user: %w", err)
			}
			u.SetTombstoned(false)

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
			log.Warnw("failed handling event", "err", err, "pdsHost", host.Host, "seq", evt.Seq, "repo", u.Did, "prev", stringLink(evt.Prev), "commit", evt.Commit.String())

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
	case env.RepoIdentity != nil:
		log.Infow("bgs got identity event", "did", env.RepoIdentity.Did)
		// Flush any cached DID documents for this user
		bgs.didr.FlushCacheFor(env.RepoIdentity.Did)

		// Refetch the DID doc and update our cached keys and handle etc.
		_, err := bgs.createExternalUser(ctx, env.RepoIdentity.Did)
		if err != nil {
			return err
		}

		// Broadcast the identity event to all consumers
		err = bgs.events.AddEvent(ctx, &events.XRPCStreamEvent{
			RepoIdentity: &comatproto.SyncSubscribeRepos_Identity{
				Did:    env.RepoIdentity.Did,
				Seq:    env.RepoIdentity.Seq,
				Time:   env.RepoIdentity.Time,
				Handle: env.RepoIdentity.Handle,
			},
		})
		if err != nil {
			log.Errorw("failed to broadcast Identity event", "error", err, "did", env.RepoIdentity.Did)
			return fmt.Errorf("failed to broadcast Identity event: %w", err)
		}

		return nil
	case env.RepoAccount != nil:
		span.SetAttributes(
			attribute.String("did", env.RepoAccount.Did),
			attribute.Int64("seq", env.RepoAccount.Seq),
			attribute.Bool("active", env.RepoAccount.Active),
		)

		if env.RepoAccount.Status != nil {
			span.SetAttributes(attribute.String("repo_status", *env.RepoAccount.Status))
		}

		log.Infow("bgs got account event", "did", env.RepoAccount.Did)
		// Flush any cached DID documents for this user
		bgs.didr.FlushCacheFor(env.RepoAccount.Did)

		// Refetch the DID doc to make sure the PDS is still authoritative
		ai, err := bgs.createExternalUser(ctx, env.RepoAccount.Did)
		if err != nil {
			span.RecordError(err)
			return err
		}

		// Check if the PDS is still authoritative
		// if not we don't want to be propagating this account event
		if ai.PDS != host.ID {
			log.Errorw("account event from non-authoritative pds",
				"seq", env.RepoAccount.Seq,
				"did", env.RepoAccount.Did,
				"event_from", host.Host,
				"did_doc_declared_pds", ai.PDS,
				"account_evt", env.RepoAccount,
			)
			return fmt.Errorf("event from non-authoritative pds")
		}

		// Process the account status change
		repoStatus := events.AccountStatusActive
		if !env.RepoAccount.Active && env.RepoAccount.Status != nil {
			repoStatus = *env.RepoAccount.Status
		}

		err = bgs.UpdateAccountStatus(ctx, env.RepoAccount.Did, repoStatus)
		if err != nil {
			span.RecordError(err)
			return fmt.Errorf("failed to update account status: %w", err)
		}

		shouldBeActive := env.RepoAccount.Active
		status := env.RepoAccount.Status
		u, err := bgs.lookupUserByDid(ctx, env.RepoAccount.Did)
		if err != nil {
			return fmt.Errorf("failed to look up user by did: %w", err)
		}

		if u.GetTakenDown() {
			shouldBeActive = false
			status = &events.AccountStatusTakendown
		}

		// Broadcast the account event to all consumers
		err = bgs.events.AddEvent(ctx, &events.XRPCStreamEvent{
			RepoAccount: &comatproto.SyncSubscribeRepos_Account{
				Did:    env.RepoAccount.Did,
				Seq:    env.RepoAccount.Seq,
				Time:   env.RepoAccount.Time,
				Active: shouldBeActive,
				Status: status,
			},
		})
		if err != nil {
			log.Errorw("failed to broadcast Account event", "error", err, "did", env.RepoAccount.Did)
			return fmt.Errorf("failed to broadcast Account event: %w", err)
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

// TODO: rename? This also updates users, and 'external' is an old phrasing
func (s *BGS) createExternalUser(ctx context.Context, did string) (*models.ActorInfo, error) {
	ctx, span := tracer.Start(ctx, "createExternalUser")
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
		peering.CrawlRateLimit = float64(s.slurper.DefaultCrawlLimit)
		peering.RateLimit = float64(s.slurper.DefaultPerSecondLimit)
		peering.HourlyEventLimit = s.slurper.DefaultPerHourLimit
		peering.DailyEventLimit = s.slurper.DefaultPerDayLimit
		peering.RepoLimit = s.slurper.DefaultRepoLimit

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

	if peering.Blocked {
		return nil, fmt.Errorf("refusing to create user with blocked PDS")
	}

	if peering.RepoCount >= peering.RepoLimit {
		return nil, fmt.Errorf("refusing to create user on PDS at max repo limit for pds %q", peering.Host)
	}

	// Increment the repo count for the PDS
	res := s.db.Model(&models.PDS{}).Where("id = ? AND repo_count < repo_limit", peering.ID).Update("repo_count", gorm.Expr("repo_count + 1"))
	if res.Error != nil {
		return nil, fmt.Errorf("failed to increment repo count for pds %q: %w", peering.Host, res.Error)
	}

	if res.RowsAffected == 0 {
		return nil, fmt.Errorf("refusing to create user on PDS at max repo limit for pds %q", peering.Host)
	}

	successfullyCreated := false

	// Release the count if we fail to create the user
	defer func() {
		if !successfullyCreated {
			if err := s.db.Model(&models.PDS{}).Where("id = ?", peering.ID).Update("repo_count", gorm.Expr("repo_count - 1")).Error; err != nil {
				log.Errorf("failed to decrement repo count for pds: %s", err)
			}
		}
	}()

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

	successfullyCreated = true

	return subj, nil
}

func (bgs *BGS) UpdateAccountStatus(ctx context.Context, did string, status string) error {
	ctx, span := tracer.Start(ctx, "UpdateAccountStatus")
	defer span.End()

	span.SetAttributes(
		attribute.String("did", did),
		attribute.String("status", status),
	)

	u, err := bgs.lookupUserByDid(ctx, did)
	if err != nil {
		return err
	}

	switch status {
	case events.AccountStatusActive:
		// Unset the PDS-specific status flags
		if err := bgs.db.Model(User{}).Where("id = ?", u.ID).Update("upstream_status", events.AccountStatusActive).Error; err != nil {
			return fmt.Errorf("failed to set user active status: %w", err)
		}
		u.SetUpstreamStatus(events.AccountStatusActive)
	case events.AccountStatusDeactivated:
		if err := bgs.db.Model(User{}).Where("id = ?", u.ID).Update("upstream_status", events.AccountStatusDeactivated).Error; err != nil {
			return fmt.Errorf("failed to set user deactivation status: %w", err)
		}
		u.SetUpstreamStatus(events.AccountStatusDeactivated)
	case events.AccountStatusSuspended:
		if err := bgs.db.Model(User{}).Where("id = ?", u.ID).Update("upstream_status", events.AccountStatusSuspended).Error; err != nil {
			return fmt.Errorf("failed to set user suspension status: %w", err)
		}
		u.SetUpstreamStatus(events.AccountStatusSuspended)
	case events.AccountStatusTakendown:
		if err := bgs.db.Model(User{}).Where("id = ?", u.ID).Update("upstream_status", events.AccountStatusTakendown).Error; err != nil {
			return fmt.Errorf("failed to set user taken down status: %w", err)
		}
		u.SetUpstreamStatus(events.AccountStatusTakendown)

		if err := bgs.db.Model(&models.ActorInfo{}).Where("uid = ?", u.ID).UpdateColumns(map[string]any{
			"handle": nil,
		}).Error; err != nil {
			return err
		}
	case events.AccountStatusDeleted:
		if err := bgs.db.Model(&User{}).Where("id = ?", u.ID).UpdateColumns(map[string]any{
			"tombstoned":      true,
			"handle":          nil,
			"upstream_status": events.AccountStatusDeleted,
		}).Error; err != nil {
			return err
		}
		u.SetUpstreamStatus(events.AccountStatusDeleted)

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
	}

	return nil
}

func (bgs *BGS) TakeDownRepo(ctx context.Context, did string) error {
	u, err := bgs.lookupUserByDid(ctx, did)
	if err != nil {
		return err
	}

	if err := bgs.db.Model(User{}).Where("id = ?", u.ID).Update("taken_down", true).Error; err != nil {
		return err
	}
	u.SetTakenDown(true)

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
	u.SetTakenDown(false)

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
	ctx, span := tracer.Start(ctx, "ResyncPDS")
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

	// run loop over repos with some concurrency
	sem := semaphore.NewWeighted(40)

	// Check repo revs against our local copy and enqueue crawls for any that are out of date
	for i, r := range repos {
		if err := sem.Acquire(ctx, 1); err != nil {
			log.Errorw("failed to acquire semaphore", "error", err)
			continue
		}
		go func(r comatproto.SyncListRepos_Repo) {
			defer sem.Release(1)
			log := log.With("did", r.Did, "remote_rev", r.Rev)
			// Fetches the user if we have it, otherwise automatically enqueues it for crawling
			ai, err := bgs.Index.GetUserOrMissing(ctx, r.Did)
			if err != nil {
				log.Errorw("failed to get user while resyncing PDS, we can't recrawl it", "error", err)
				return
			}

			rev, err := bgs.repoman.GetRepoRev(ctx, ai.Uid)
			if err != nil {
				log.Warnw("recrawling because we failed to get the local repo root", "err", err, "uid", ai.Uid)
				err := bgs.Index.Crawler.Crawl(ctx, ai)
				if err != nil {
					log.Errorw("failed to enqueue crawl for repo during resync", "error", err, "uid", ai.Uid, "did", ai.Did)
				}
				return
			}

			if rev == "" || rev < r.Rev {
				log.Warnw("recrawling because the repo rev from the PDS is newer than our local repo rev", "local_rev", rev)
				err := bgs.Index.Crawler.Crawl(ctx, ai)
				if err != nil {
					log.Errorw("failed to enqueue crawl for repo during resync", "error", err, "uid", ai.Uid, "did", ai.Did)
				}
				return
			}
		}(r)
		if i%100 == 0 {
			if i%10_000 == 0 {
				log.Warnw("checked revs during resync", "num_repos_checked", i, "num_repos_to_crawl", -1, "took", time.Now().Sub(resync.StatusChangedAt))
			}
			resync.NumReposChecked = i
			bgs.UpdateResync(resync)
		}
	}

	resync.NumReposChecked = len(repos)
	bgs.UpdateResync(resync)

	log.Warnw("enqueued all crawls, exiting resync", "took", time.Now().Sub(start), "num_repos_to_crawl", -1)

	return nil
}
