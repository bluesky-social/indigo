package bgs

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"github.com/bluesky-social/indigo/atproto/identity"
	"github.com/bluesky-social/indigo/atproto/syntax"
	"github.com/ipfs/go-cid"
	"io"
	"log/slog"
	"net"
	"net/http"
	_ "net/http/pprof"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	comatproto "github.com/bluesky-social/indigo/api/atproto"
	"github.com/bluesky-social/indigo/cmd/medsky/events"
	"github.com/bluesky-social/indigo/cmd/medsky/models"
	"github.com/bluesky-social/indigo/cmd/medsky/repomgr"
	lexutil "github.com/bluesky-social/indigo/lex/util"
	"github.com/bluesky-social/indigo/xrpc"

	"github.com/gorilla/websocket"
	lru "github.com/hashicorp/golang-lru/v2"
	"github.com/labstack/echo/v4"
	"github.com/labstack/echo/v4/middleware"
	promclient "github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	dto "github.com/prometheus/client_model/go"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"gorm.io/gorm"
)

var tracer = otel.Tracer("bgs")

// serverListenerBootTimeout is how long to wait for the requested server socket
// to become available for use. This is an arbitrary timeout that should be safe
// on any platform, but there's no great way to weave this timeout without
// adding another parameter to the (at time of writing) long signature of
// NewServer.
const serverListenerBootTimeout = 5 * time.Second

type BGS struct {
	db      *gorm.DB
	slurper *Slurper
	events  *events.EventManager
	didd    identity.Directory

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

	// User cache
	userCache *lru.Cache[string, *User]

	// nextCrawlers gets forwarded POST /xrpc/com.atproto.sync.requestCrawl
	nextCrawlers []*url.URL
	httpClient   http.Client

	log               *slog.Logger
	inductionTraceLog *slog.Logger

	config BGSConfig
}

type SocketConsumer struct {
	UserAgent   string
	RemoteAddr  string
	ConnectedAt time.Time
	EventsSent  promclient.Counter
}

type BGSConfig struct {
	SSL               bool
	DefaultRepoLimit  int64
	ConcurrencyPerPDS int64
	MaxQueuePerPDS    int64

	// NextCrawlers gets forwarded POST /xrpc/com.atproto.sync.requestCrawl
	NextCrawlers []*url.URL

	ApplyPDSClientSettings func(c *xrpc.Client)
	InductionTraceLog      *slog.Logger
}

func DefaultBGSConfig() *BGSConfig {
	return &BGSConfig{
		SSL:               true,
		DefaultRepoLimit:  100,
		ConcurrencyPerPDS: 100,
		MaxQueuePerPDS:    1_000,
	}
}

func NewBGS(db *gorm.DB, repoman *repomgr.RepoManager, evtman *events.EventManager, didd identity.Directory, config *BGSConfig) (*BGS, error) {

	if config == nil {
		config = DefaultBGSConfig()
	}
	if err := db.AutoMigrate(AuthToken{}); err != nil {
		panic(err)
	}
	if err := db.AutoMigrate(DomainBan{}); err != nil {
		panic(err)
	}
	if err := db.AutoMigrate(models.PDS{}); err != nil {
		panic(err)
	}
	if err := db.AutoMigrate(User{}); err != nil {
		panic(err)
	}
	if err := db.AutoMigrate(UserPreviousState{}); err != nil {
		panic(err)
	}

	uc, _ := lru.New[string, *User](1_000_000)

	bgs := &BGS{
		db: db,

		repoman: repoman,
		events:  evtman,
		didd:    didd,
		ssl:     config.SSL,

		consumersLk: sync.RWMutex{},
		consumers:   make(map[uint64]*SocketConsumer),

		userCache: uc,

		log: slog.Default().With("system", "bgs"),

		config: *config,

		inductionTraceLog: config.InductionTraceLog,
	}

	slOpts := DefaultSlurperOptions()
	slOpts.SSL = config.SSL
	slOpts.DefaultRepoLimit = config.DefaultRepoLimit
	slOpts.ConcurrencyPerPDS = config.ConcurrencyPerPDS
	slOpts.MaxQueuePerPDS = config.MaxQueuePerPDS
	slOpts.Logger = bgs.log
	s, err := NewSlurper(db, bgs.handleFedEvent, slOpts)
	if err != nil {
		return nil, err
	}

	bgs.slurper = s

	if err := bgs.slurper.RestartAll(); err != nil {
		return nil, err
	}

	bgs.nextCrawlers = config.NextCrawlers
	bgs.httpClient.Timeout = time.Second * 5

	return bgs, nil
}

func (bgs *BGS) StartMetrics(listen string) error {
	http.Handle("/metrics", promhttp.Handler())
	return http.ListenAndServe(listen, nil)
}

func (bgs *BGS) Start(addr string, logWriter io.Writer) error {
	var lc net.ListenConfig
	ctx, cancel := context.WithTimeout(context.Background(), serverListenerBootTimeout)
	defer cancel()

	li, err := lc.Listen(ctx, "tcp", addr)
	if err != nil {
		return err
	}
	return bgs.StartWithListener(li, logWriter)
}

func (bgs *BGS) StartWithListener(listen net.Listener, logWriter io.Writer) error {
	e := echo.New()
	e.Logger.SetOutput(logWriter)
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
				bgs.log.Error("Failed to write http error", "err", err2)
			}
		default:
			sendHeader := true
			if ctx.Path() == "/xrpc/com.atproto.sync.subscribeRepos" {
				sendHeader = false
			}

			bgs.log.Warn("HANDLER ERROR: (%s) %s", ctx.Path(), err)

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
	e.POST("/xrpc/com.atproto.sync.requestCrawl", bgs.HandleComAtprotoSyncRequestCrawl)
	e.GET("/xrpc/com.atproto.sync.listRepos", bgs.HandleComAtprotoSyncListRepos)
	e.GET("/xrpc/com.atproto.sync.getRepo", bgs.HandleComAtprotoSyncGetRepo) // just returns 3xx redirect to source PDS
	e.GET("/xrpc/com.atproto.sync.getLatestCommit", bgs.HandleComAtprotoSyncGetLatestCommit)
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
	admin.GET("/repo/takedowns", bgs.handleAdminListRepoTakeDowns)

	// PDS-related Admin API
	admin.POST("/pds/requestCrawl", bgs.handleAdminRequestCrawl)
	admin.GET("/pds/list", bgs.handleListPDSs)
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

	return errs
}

type HealthStatus struct {
	Status  string `json:"status"`
	Message string `json:"msg,omitempty"`
}

func (bgs *BGS) HandleHealthCheck(c echo.Context) error {
	if err := bgs.db.Exec("SELECT 1").Error; err != nil {
		bgs.log.Error("healthcheck can't connect to database", "err", err)
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

func (u *User) GetDid() string {
	return u.Did
}

func (u *User) GetUid() models.Uid {
	return u.ID
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

type UserPreviousState struct {
	Uid models.Uid   `gorm:"column:uid;primaryKey"`
	Cid models.DbCID `gorm:"column:cid"`
	Rev string       `gorm:"column:rev"`
	Seq int64        `gorm:"column:seq"`
}

func (ups *UserPreviousState) GetCid() cid.Cid {
	return ups.Cid.CID
}
func (ups *UserPreviousState) GetRev() syntax.TID {
	xt, _ := syntax.ParseTID(ups.Rev)
	return xt
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
		bgs.log.Error("failed to get sent counter", "err", err)
	}

	bgs.log.Info("consumer disconnected",
		"consumer_id", id,
		"remote_addr", c.RemoteAddr,
		"user_agent", c.UserAgent,
		"events_sent", m.Counter.GetValue())

	delete(bgs.consumers, id)
}

// GET+websocket /xrpc/com.atproto.sync.subscribeRepos
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
					bgs.log.Warn("failed to ping client", "err", err)
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
				bgs.log.Warn("failed to read message from client", "err", err)
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

	logger := bgs.log.With(
		"consumer_id", consumerID,
		"remote_addr", consumer.RemoteAddr,
		"user_agent", consumer.UserAgent,
	)

	logger.Info("new consumer", "cursor", since)

	for {
		select {
		case evt, ok := <-evts:
			if !ok {
				logger.Error("event stream closed unexpectedly")
				return nil
			}

			wc, err := conn.NextWriter(websocket.BinaryMessage)
			if err != nil {
				logger.Error("failed to get next writer", "err", err)
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
				logger.Warn("failed to flush-close our event write", "err", err)
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
	var db DomainBan
	if err := s.db.Find(&db, "domain = ?", host).Error; err != nil {
		return false, err
	}

	if db.ID == 0 {
		return false, nil
	}

	return true, nil
}

var ErrNotFound = errors.New("not found")

func (bgs *BGS) DidToUid(ctx context.Context, did string) (models.Uid, error) {
	xu, err := bgs.lookupUserByDid(ctx, did)
	if err != nil {
		return 0, err
	}
	if xu == nil {
		return 0, ErrNotFound
	}
	return xu.ID, nil
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

// handleFedEvent() is the callback passed to Slurper called from Slurper.handleConnection()
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
		return bgs.handleCommit(ctx, host, env.RepoCommit)
	case env.RepoSync != nil:
		return bgs.handleSync(ctx, host, env.RepoSync)
	case env.RepoHandle != nil:
		// TODO: DEPRECATED - expect Identity message below instead
		bgs.log.Info("bgs got repo handle event", "did", env.RepoHandle.Did, "handle", env.RepoHandle.Handle)
		// Flush any cached DID documents for this user
		bgs.purgeDidCache(ctx, env.RepoHandle.Did)

		// TODO: ignoring the data in the message and just going out to the DID doc
		act, err := bgs.createExternalUser(ctx, env.RepoHandle.Did, host)
		if err != nil {
			return err
		}

		if act.Handle.String != env.RepoHandle.Handle {
			bgs.log.Warn("handle update did not update handle to asserted value", "did", env.RepoHandle.Did, "expected", env.RepoHandle.Handle, "actual", act.Handle)
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
			bgs.log.Error("failed to broadcast RepoHandle event", "error", err, "did", env.RepoHandle.Did, "handle", env.RepoHandle.Handle)
			return fmt.Errorf("failed to broadcast RepoHandle event: %w", err)
		}

		return nil
	case env.RepoIdentity != nil:
		bgs.log.Info("bgs got identity event", "did", env.RepoIdentity.Did)
		// Flush any cached DID documents for this user
		bgs.purgeDidCache(ctx, env.RepoIdentity.Did)

		// Refetch the DID doc and update our cached keys and handle etc.
		_, err := bgs.createExternalUser(ctx, env.RepoIdentity.Did, host)
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
			bgs.log.Error("failed to broadcast Identity event", "error", err, "did", env.RepoIdentity.Did)
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

		bgs.log.Info("bgs got account event", "did", env.RepoAccount.Did)
		// Flush any cached DID documents for this user
		bgs.purgeDidCache(ctx, env.RepoAccount.Did)

		// Refetch the DID doc to make sure the PDS is still authoritative
		ai, err := bgs.createExternalUser(ctx, env.RepoAccount.Did, host)
		if err != nil {
			span.RecordError(err)
			return err
		}

		// Check if the PDS is still authoritative
		// if not we don't want to be propagating this account event
		if ai.PDS != host.ID {
			bgs.log.Error("account event from non-authoritative pds",
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
			bgs.log.Error("failed to broadcast Account event", "error", err, "did", env.RepoAccount.Did)
			return fmt.Errorf("failed to broadcast Account event: %w", err)
		}

		return nil
	case env.RepoMigrate != nil:
		// TODO: DEPRECATED - expect account message above instead
		if _, err := bgs.createExternalUser(ctx, env.RepoMigrate.Did, host); err != nil {
			return err
		}

		return nil
	case env.RepoTombstone != nil:
		// TODO: DEPRECATED - expect account message above instead
		if err := bgs.handleRepoTombstone(ctx, host, env.RepoTombstone); err != nil {
			return err
		}

		return nil
	default:
		return fmt.Errorf("invalid fed event")
	}
}

func (bgs *BGS) handleCommit(ctx context.Context, host *models.PDS, evt *comatproto.SyncSubscribeRepos_Commit) error {
	bgs.log.Debug("bgs got repo append event", "seq", evt.Seq, "pdsHost", host.Host, "repo", evt.Repo)

	//s := time.Now()
	u, err := bgs.lookupUserByDid(ctx, evt.Repo)
	//userLookupDuration.Observe(time.Since(s).Seconds())
	if err != nil {
		if !errors.Is(err, gorm.ErrRecordNotFound) {
			repoCommitsResultCounter.WithLabelValues(host.Host, "nou").Inc()
			return fmt.Errorf("looking up event user: %w", err)
		}

		newUsersDiscovered.Inc()
		start := time.Now()
		subj, err := bgs.createExternalUser(ctx, evt.Repo, host)
		newUserDiscoveryDuration.Observe(time.Since(start).Seconds())
		if err != nil {
			repoCommitsResultCounter.WithLabelValues(host.Host, "uerr").Inc()
			return fmt.Errorf("fed event create external user: %w", err)
		}

		u = subj
	}

	ustatus := u.GetUpstreamStatus()
	//span.SetAttributes(attribute.String("upstream_status", ustatus))

	if u.GetTakenDown() || ustatus == events.AccountStatusTakendown {
		//span.SetAttributes(attribute.Bool("taken_down_by_relay_admin", u.GetTakenDown()))
		bgs.log.Debug("dropping commit event from taken down user", "did", evt.Repo, "seq", evt.Seq, "pdsHost", host.Host)
		repoCommitsResultCounter.WithLabelValues(host.Host, "tdu").Inc()
		return nil
	}

	if ustatus == events.AccountStatusSuspended {
		bgs.log.Debug("dropping commit event from suspended user", "did", evt.Repo, "seq", evt.Seq, "pdsHost", host.Host)
		repoCommitsResultCounter.WithLabelValues(host.Host, "susu").Inc()
		return nil
	}

	if ustatus == events.AccountStatusDeactivated {
		bgs.log.Debug("dropping commit event from deactivated user", "did", evt.Repo, "seq", evt.Seq, "pdsHost", host.Host)
		repoCommitsResultCounter.WithLabelValues(host.Host, "du").Inc()
		return nil
	}

	if evt.Rebase {
		repoCommitsResultCounter.WithLabelValues(host.Host, "rebase").Inc()
		return fmt.Errorf("rebase was true in event seq:%d,host:%s", evt.Seq, host.Host)
	}

	if host.ID != u.PDS && u.PDS != 0 {
		bgs.log.Warn("received event for repo from different pds than expected", "repo", evt.Repo, "expPds", u.PDS, "gotPds", host.Host)
		// Flush any cached DID documents for this user
		bgs.purgeDidCache(ctx, evt.Repo)

		subj, err := bgs.createExternalUser(ctx, evt.Repo, host)
		if err != nil {
			repoCommitsResultCounter.WithLabelValues(host.Host, "uerr2").Inc()
			return err
		}

		if subj.PDS != host.ID {
			repoCommitsResultCounter.WithLabelValues(host.Host, "noauth").Inc()
			return fmt.Errorf("event from non-authoritative pds")
		}
	}

	if u.GetTombstoned() {
		// TODO: reevaluate user lifecycle - tombstoned -- bolson 2025

		//span.SetAttributes(attribute.Bool("tombstoned", true))
		// we've checked the authority of the users PDS, so reinstate the account
		if err := bgs.db.Model(&User{}).Where("id = ?", u.ID).UpdateColumn("tombstoned", false).Error; err != nil {
			repoCommitsResultCounter.WithLabelValues(host.Host, "tomb").Inc()
			return fmt.Errorf("failed to un-tombstone a user: %w", err)
		}
		u.SetTombstoned(false)

		//ai, err := bgs.Index.LookupUser(ctx, u.ID)
		//if err != nil {
		//	repoCommitsResultCounter.WithLabelValues(host.Host, "nou2").Inc()
		//	return fmt.Errorf("failed to look up user (tombstone recover): %w", err)
		//}

		// Now a simple re-crawl should suffice to bring the user back online
		//repoCommitsResultCounter.WithLabelValues(host.Host, "catchupt").Inc()
		//return bgs.Index.Crawler.AddToCatchupQueue(ctx, host, ai, evt)
		// TODO: fall through and just handle the event and the right thing should happen? -- bolson 2025 unsure
	}

	var prevState UserPreviousState
	err = bgs.db.First(&prevState, u.ID).Error
	//prevP := &prevState
	var prevP repomgr.UserPrev = &prevState
	if errors.Is(err, gorm.ErrRecordNotFound) {
		prevP = nil
	} else if err != nil {
		bgs.log.Error("failed to get previous root", "err", err)
		prevP = nil
	}
	dbPrevRootStr := ""
	dbPrevSeqStr := ""
	if prevP != nil {
		if prevState.Seq >= evt.Seq && ((prevState.Seq - evt.Seq) < 2000) {
			// ignore catchup overlap of 200 on some subscribeRepos restarts
			repoCommitsResultCounter.WithLabelValues(host.Host, "dup").Inc()
			return nil
		}
		dbPrevRootStr = prevState.Cid.CID.String()
		dbPrevSeqStr = strconv.FormatInt(prevState.Seq, 10)
	}
	evtPrevDataStr := ""
	if evt.PrevData != nil {
		evtPrevDataStr = ((*cid.Cid)(evt.PrevData)).String()
	}
	newRootCid, err := bgs.repoman.HandleCommit(ctx, host, u, evt, prevP)
	if err != nil {
		bgs.inductionTraceLog.Error("commit bad", "seq", evt.Seq, "pseq", dbPrevSeqStr, "pdsHost", host.Host, "repo", evt.Repo, "prev", evtPrevDataStr, "dbprev", dbPrevRootStr, "err", err)
		bgs.log.Warn("failed handling event", "err", err, "pdsHost", host.Host, "seq", evt.Seq, "repo", u.Did, "commit", evt.Commit.String())
		repoCommitsResultCounter.WithLabelValues(host.Host, "err").Inc()
		return fmt.Errorf("handle user event failed: %w", err)
	} else {
		// store now verified new repo state
		prevState.Uid = u.ID
		prevState.Cid.CID = *newRootCid
		prevState.Rev = evt.Rev
		prevState.Seq = evt.Seq
		bgs.inductionTraceLog.Info("commit  ok", "seq", evt.Seq, "pdsHost", host.Host, "repo", evt.Repo, "prev", evtPrevDataStr, "dbprev", dbPrevRootStr, "nextprev", newRootCid.String())
		if prevP == nil {
			err = bgs.db.Create(&prevState).Error
			if err != nil {
				return fmt.Errorf("failed to create previous root uid=%d: %w", u.ID, err)
			}
		} else {
			err = bgs.db.Save(&prevState).Error
			if err != nil {
				return fmt.Errorf("failed to save previous root uid=%d: %w", u.ID, err)
			}
		}
	}

	repoCommitsResultCounter.WithLabelValues(host.Host, "ok").Inc()
	return nil
}

func (bgs *BGS) handleSync(ctx context.Context, host *models.PDS, evt *comatproto.SyncSubscribeRepos_Sync) error {
	// TODO: actually do something with #sync event

	// Broadcast the identity event to all consumers
	err := bgs.events.AddEvent(ctx, &events.XRPCStreamEvent{
		RepoSync: evt,
	})
	if err != nil {
		bgs.log.Error("failed to broadcast sync event", "error", err, "did", evt.Did)
		return fmt.Errorf("failed to broadcast sync event: %w", err)
	}

	return nil
}

func (bgs *BGS) handleRepoTombstone(ctx context.Context, pds *models.PDS, evt *comatproto.SyncSubscribeRepos_Tombstone) error {
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
	u.SetTombstoned(true)

	return bgs.events.AddEvent(ctx, &events.XRPCStreamEvent{
		RepoTombstone: evt,
	})
}

func (bgs *BGS) purgeDidCache(ctx context.Context, did string) {
	ati, err := syntax.ParseAtIdentifier(did)
	if err != nil {
		return
	}
	_ = bgs.didd.Purge(ctx, *ati)
}

// createExternalUser is a mess and poorly defined
// did is the user
// host is the PDS we received this from, not necessarily the canonical PDS in the DID document
// TODO: rename? This also updates users, and 'external' is an old phrasing
func (bgs *BGS) createExternalUser(ctx context.Context, did string, host *models.PDS) (*User, error) {
	ctx, span := tracer.Start(ctx, "createExternalUser")
	defer span.End()

	externalUserCreationAttempts.Inc()

	bgs.log.Debug("create external user", "did", did)
	pdid, err := syntax.ParseDID(did)
	if err != nil {
		return nil, fmt.Errorf("bad did %#v, %w", did, err)
	}
	ident, err := bgs.didd.LookupDID(ctx, pdid)
	if err != nil {
		return nil, fmt.Errorf("no ident for did %s, %w", did, err)
	}
	if len(ident.Services) == 0 {
		return nil, fmt.Errorf("no services for did %s", did)
	}
	pdsService, ok := ident.Services["atproto_pds"]
	if !ok {
		return nil, fmt.Errorf("no atproto_pds service for did %s", did)
	}
	durl, err := url.Parse(pdsService.URL)
	if err != nil {
		return nil, fmt.Errorf("pds bad url %#v, %w", pdsService.URL, err)
	}

	if strings.HasPrefix(durl.Host, "localhost:") {
		durl.Scheme = "http"
	}

	// TODO: the PDS's DID should also be in the service, we could use that to look up?
	var peering models.PDS
	if err := bgs.db.Find(&peering, "host = ?", durl.Host).Error; err != nil {
		bgs.log.Error("failed to find pds", "host", durl.Host)
		return nil, err
	}

	ban, err := bgs.domainIsBanned(ctx, durl.Host)
	if err != nil {
		return nil, fmt.Errorf("failed to check pds ban status: %w", err)
	}

	if ban {
		return nil, fmt.Errorf("cannot create user on pds with banned domain")
	}

	if peering.ID == 0 {
		// why didn't we know about this PDS from requestCrawl?
		// _maybe this never happens_ ? because of above peering Find ?
		bgs.log.Warn("pds discovered in new user flow", "pds", durl.String(), "did", did)
		pclient := &xrpc.Client{Host: durl.String()}
		bgs.config.ApplyPDSClientSettings(pclient)
		// TODO: the case of handling a new user on a new PDS probably requires more thought
		cfg, err := comatproto.ServerDescribeServer(ctx, pclient)
		if err != nil {
			// TODO: failing this shouldn't halt our indexing
			return nil, fmt.Errorf("failed to check unrecognized pds: %w", err)
		}

		// since handles can be anything, checking against this list doesn't matter...
		_ = cfg

		// TODO: could check other things, a valid response is good enough for now
		peering.Host = durl.Host
		peering.SSL = (durl.Scheme == "https")
		peering.RateLimit = float64(bgs.slurper.DefaultPerSecondLimit)
		peering.HourlyEventLimit = bgs.slurper.DefaultPerHourLimit
		peering.DailyEventLimit = bgs.slurper.DefaultPerDayLimit
		peering.RepoLimit = bgs.slurper.DefaultRepoLimit

		if bgs.ssl && !peering.SSL {
			return nil, fmt.Errorf("did references non-ssl PDS, this is disallowed in prod: %q %q", did, pdsService.URL)
		}

		if err := bgs.db.Create(&peering).Error; err != nil {
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

	bgs.extUserLk.Lock()
	defer bgs.extUserLk.Unlock()

	user, err := bgs.lookupUserByDid(ctx, did)
	if err == nil {
		bgs.log.Debug("lost the race to create a new user", "did", did, "existing_hand", user.Handle)
		if user.PDS != peering.ID {
			// User is now on a different PDS, update
			err = bgs.db.Transaction(func(tx *gorm.DB) error {
				res := tx.Model(User{}).Where("id = ?", user.ID).Update("pds", peering.ID)
				if res.Error != nil {
					return fmt.Errorf("failed to update users pds: %w", res.Error)
				}
				res = tx.Model(&models.PDS{}).Where("id = ? AND repo_count < repo_limit", peering.ID).Update("repo_count", gorm.Expr("repo_count + 1"))
				return nil
			})

			user.PDS = peering.ID
		}
		return user, nil
	}

	if !errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, err
	}

	// TODO: request this users info from their server to fill out our data...
	u := User{
		Did:         did,
		PDS:         peering.ID,
		ValidHandle: false,
	}

	err = bgs.db.Transaction(func(tx *gorm.DB) error {
		res := tx.Model(&models.PDS{}).Where("id = ? AND repo_count < repo_limit", peering.ID).Update("repo_count", gorm.Expr("repo_count + 1"))
		if res.Error != nil {
			return fmt.Errorf("failed to increment repo count for pds %q: %w", peering.Host, res.Error)
		}
		if terr := bgs.db.Create(&u).Error; terr != nil {
			bgs.log.Error("failed to create user", "did", u.Did, "err", terr)
			return fmt.Errorf("failed to create other pds user: %w", terr)
		}
		return nil
	})
	if err != nil {
		bgs.log.Error("user creaat and pds inc err", "err", err)
		return nil, err
	}

	return &u, nil
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
		// TODO: set User takedown in db? -- bolson 2025
	case events.AccountStatusDeleted:
		// TODO: tweak model to mark user deleted? -- bolson 2025
		if err := bgs.db.Model(&User{}).Where("id = ?", u.ID).UpdateColumns(map[string]any{
			"tombstoned":      true,
			"handle":          nil,
			"upstream_status": events.AccountStatusDeleted,
		}).Error; err != nil {
			return err
		}
		u.SetUpstreamStatus(events.AccountStatusDeleted)
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

func (bgs *BGS) GetRepoRoot(ctx context.Context, user models.Uid) (cid.Cid, error) {
	var prevState UserPreviousState
	err := bgs.db.First(&prevState, user).Error
	if err == nil {
		return prevState.Cid.CID, nil
	} else if errors.Is(err, gorm.ErrRecordNotFound) {
		return cid.Cid{}, ErrUserStatusUnavailable
	} else {
		bgs.log.Error("user db err", "err", err)
		return cid.Cid{}, fmt.Errorf("user prev db err, %w", err)
	}
}
