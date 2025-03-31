package main

import (
	"context"
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
	"github.com/bluesky-social/indigo/cmd/relayered/models"
	"github.com/bluesky-social/indigo/cmd/relayered/slurper"
	"github.com/bluesky-social/indigo/cmd/relayered/stream"
	"github.com/bluesky-social/indigo/cmd/relayered/stream/eventmgr"
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

var tracer = otel.Tracer("relay")

// serverListenerBootTimeout is how long to wait for the requested server socket
// to become available for use. This is an arbitrary timeout that should be safe
// on any platform, but there's no great way to weave this timeout without
// adding another parameter to the (at time of writing) long signature of
// NewServer.
const serverListenerBootTimeout = 5 * time.Second

type Service struct {
	db      *gorm.DB
	slurper *slurper.Slurper
	events  *eventmgr.EventManager
	dir     identity.Directory

	// TODO: work on doing away with this flag in favor of more pluggable
	// pieces that abstract the need for explicit ssl checks
	ssl bool

	// extUserLk serializes a section of syncPDSAccount()
	// TODO: at some point we will want to lock specific DIDs, this lock as is
	// is overly broad, but i dont expect it to be a bottleneck for now
	extUserLk sync.Mutex

	validator *slurper.Validator

	// Management of Socket Consumers
	consumersLk    sync.RWMutex
	nextConsumerID uint64
	consumers      map[uint64]*SocketConsumer

	// Account cache
	userCache *lru.Cache[string, *slurper.Account]

	// nextCrawlers gets forwarded POST /xrpc/com.atproto.sync.requestCrawl
	nextCrawlers []*url.URL
	httpClient   http.Client

	log               *slog.Logger
	inductionTraceLog *slog.Logger

	config RelayConfig
}

type SocketConsumer struct {
	UserAgent   string
	RemoteAddr  string
	ConnectedAt time.Time
	EventsSent  promclient.Counter
}

type RelayConfig struct {
	SSL               bool
	DefaultRepoLimit  int64
	ConcurrencyPerPDS int64
	MaxQueuePerPDS    int64

	// NextCrawlers gets forwarded POST /xrpc/com.atproto.sync.requestCrawl
	NextCrawlers []*url.URL

	ApplyPDSClientSettings func(c *xrpc.Client)
	InductionTraceLog      *slog.Logger

	// AdminToken checked against "Authorization: Bearer {}" header
	AdminToken string
}

func DefaultRelayConfig() *RelayConfig {
	return &RelayConfig{
		SSL:               true,
		DefaultRepoLimit:  100,
		ConcurrencyPerPDS: 100,
		MaxQueuePerPDS:    1_000,
	}
}

func NewService(db *gorm.DB, validator *slurper.Validator, evtman *eventmgr.EventManager, dir identity.Directory, config *RelayConfig) (*Service, error) {

	if config == nil {
		config = DefaultRelayConfig()
	}
	if err := db.AutoMigrate(slurper.DomainBan{}); err != nil {
		panic(err)
	}
	if err := db.AutoMigrate(slurper.PDS{}); err != nil {
		panic(err)
	}
	if err := db.AutoMigrate(slurper.Account{}); err != nil {
		panic(err)
	}
	if err := db.AutoMigrate(slurper.AccountPreviousState{}); err != nil {
		panic(err)
	}

	uc, _ := lru.New[string, *slurper.Account](1_000_000)

	svc := &Service{
		db: db,

		validator: validator,
		events:    evtman,
		dir:       dir,
		ssl:       config.SSL,

		consumersLk: sync.RWMutex{},
		consumers:   make(map[uint64]*SocketConsumer),

		userCache: uc,

		log: slog.Default().With("system", "relay"),

		config: *config,

		inductionTraceLog: config.InductionTraceLog,
	}

	slOpts := slurper.DefaultSlurperOptions()
	slOpts.SSL = config.SSL
	slOpts.DefaultRepoLimit = config.DefaultRepoLimit
	slOpts.ConcurrencyPerPDS = config.ConcurrencyPerPDS
	slOpts.MaxQueuePerPDS = config.MaxQueuePerPDS
	slOpts.Logger = svc.log
	s, err := slurper.NewSlurper(db, svc.handleFedEvent, slOpts)
	if err != nil {
		return nil, err
	}

	svc.slurper = s

	if err := svc.slurper.RestartAll(); err != nil {
		return nil, err
	}

	svc.nextCrawlers = config.NextCrawlers
	svc.httpClient.Timeout = time.Second * 5

	return svc, nil
}

func (svc *Service) StartMetrics(listen string) error {
	http.Handle("/metrics", promhttp.Handler())
	return http.ListenAndServe(listen, nil)
}

func (svc *Service) Start(addr string, logWriter io.Writer) error {
	var lc net.ListenConfig
	ctx, cancel := context.WithTimeout(context.Background(), serverListenerBootTimeout)
	defer cancel()

	li, err := lc.Listen(ctx, "tcp", addr)
	if err != nil {
		return err
	}
	return svc.StartWithListener(li, logWriter)
}

func (svc *Service) StartWithListener(listen net.Listener, logWriter io.Writer) error {
	e := echo.New()
	e.Logger.SetOutput(logWriter)
	e.HideBanner = true

	e.Use(middleware.CORSWithConfig(middleware.CORSConfig{
		AllowOrigins: []string{"*"},
		AllowHeaders: []string{echo.HeaderOrigin, echo.HeaderContentType, echo.HeaderAccept, echo.HeaderAuthorization},
	}))

	if !svc.ssl {
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
				svc.log.Error("Failed to write http error", "err", err2)
			}
		default:
			sendHeader := true
			if ctx.Path() == "/xrpc/com.atproto.sync.subscribeRepos" {
				sendHeader = false
			}

			svc.log.Warn("HANDLER ERROR: (%s) %s", ctx.Path(), err)

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

	e.GET("/xrpc/com.atproto.sync.subscribeRepos", svc.EventsHandler)
	e.POST("/xrpc/com.atproto.sync.requestCrawl", svc.HandleComAtprotoSyncRequestCrawl)
	e.GET("/xrpc/com.atproto.sync.listRepos", svc.HandleComAtprotoSyncListRepos)
	e.GET("/xrpc/com.atproto.sync.getRepo", svc.HandleComAtprotoSyncGetRepo) // just returns 3xx redirect to source PDS
	e.GET("/xrpc/com.atproto.sync.getLatestCommit", svc.HandleComAtprotoSyncGetLatestCommit)
	e.GET("/xrpc/_health", svc.HandleHealthCheck)
	e.GET("/_health", svc.HandleHealthCheck)
	e.GET("/", svc.HandleHomeMessage)

	admin := e.Group("/admin", svc.checkAdminAuth)

	// Slurper-related Admin API
	admin.GET("/subs/getUpstreamConns", svc.handleAdminGetUpstreamConns)
	admin.GET("/subs/getEnabled", svc.handleAdminGetSubsEnabled)
	admin.GET("/subs/perDayLimit", svc.handleAdminGetNewPDSPerDayRateLimit)
	admin.POST("/subs/setEnabled", svc.handleAdminSetSubsEnabled)
	admin.POST("/subs/killUpstream", svc.handleAdminKillUpstreamConn)
	admin.POST("/subs/setPerDayLimit", svc.handleAdminSetNewPDSPerDayRateLimit)

	// Domain-related Admin API
	admin.GET("/subs/listDomainBans", svc.handleAdminListDomainBans)
	admin.POST("/subs/banDomain", svc.handleAdminBanDomain)
	admin.POST("/subs/unbanDomain", svc.handleAdminUnbanDomain)

	// Repo-related Admin API
	admin.POST("/repo/takeDown", svc.handleAdminTakeDownRepo)
	admin.POST("/repo/reverseTakedown", svc.handleAdminReverseTakedown)
	admin.GET("/repo/takedowns", svc.handleAdminListRepoTakeDowns)

	// PDS-related Admin API
	admin.POST("/pds/requestCrawl", svc.handleAdminRequestCrawl)
	admin.GET("/pds/list", svc.handleListPDSs)
	admin.POST("/pds/changeLimits", svc.handleAdminChangePDSRateLimits)
	admin.POST("/pds/block", svc.handleBlockPDS)
	admin.POST("/pds/unblock", svc.handleUnblockPDS)
	admin.POST("/pds/addTrustedDomain", svc.handleAdminAddTrustedDomain)

	// Consumer-related Admin API
	admin.GET("/consumers/list", svc.handleAdminListConsumers)

	// In order to support booting on random ports in tests, we need to tell the
	// Echo instance it's already got a port, and then use its StartServer
	// method to re-use that listener.
	e.Listener = listen
	srv := &http.Server{}
	return e.StartServer(srv)
}

func (svc *Service) Shutdown() []error {
	errs := svc.slurper.Shutdown()

	if err := svc.events.Shutdown(context.TODO()); err != nil {
		errs = append(errs, err)
	}

	return errs
}

type HealthStatus struct {
	Status  string `json:"status"`
	Message string `json:"msg,omitempty"`
}

func (svc *Service) HandleHealthCheck(c echo.Context) error {
	if err := svc.db.Exec("SELECT 1").Error; err != nil {
		svc.log.Error("healthcheck can't connect to database", "err", err)
		return c.JSON(500, HealthStatus{Status: "error", Message: "can't connect to database"})
	} else {
		return c.JSON(200, HealthStatus{Status: "ok"})
	}
}

var homeMessage string = `
.########..########.##..........###....##....##
.##.....##.##.......##.........##.##....##..##.
.##.....##.##.......##........##...##....####..
.########..######...##.......##.....##....##...
.##...##...##.......##.......#########....##...
.##....##..##.......##.......##.....##....##...
.##.....##.########.########.##.....##....##...

This is an atproto [https://atproto.com] relay instance, running the 'relay' codebase [https://github.com/bluesky-social/indigo]

The firehose WebSocket path is at:  /xrpc/com.atproto.sync.subscribeRepos
`

func (svc *Service) HandleHomeMessage(c echo.Context) error {
	return c.String(http.StatusOK, homeMessage)
}

const authorizationBearerPrefix = "Bearer "

func (svc *Service) checkAdminAuth(next echo.HandlerFunc) echo.HandlerFunc {
	return func(e echo.Context) error {
		authheader := e.Request().Header.Get("Authorization")
		if !strings.HasPrefix(authheader, authorizationBearerPrefix) {
			return echo.ErrForbidden
		}

		token := authheader[len(authorizationBearerPrefix):]

		if svc.config.AdminToken != token {
			return echo.ErrForbidden
		}

		return next(e)
	}
}

type addTargetBody struct {
	Host string `json:"host"`
}

func (svc *Service) registerConsumer(c *SocketConsumer) uint64 {
	svc.consumersLk.Lock()
	defer svc.consumersLk.Unlock()

	id := svc.nextConsumerID
	svc.nextConsumerID++

	svc.consumers[id] = c

	return id
}

func (svc *Service) cleanupConsumer(id uint64) {
	svc.consumersLk.Lock()
	defer svc.consumersLk.Unlock()

	c := svc.consumers[id]

	var m = &dto.Metric{}
	if err := c.EventsSent.Write(m); err != nil {
		svc.log.Error("failed to get sent counter", "err", err)
	}

	svc.log.Info("consumer disconnected",
		"consumer_id", id,
		"remote_addr", c.RemoteAddr,
		"user_agent", c.UserAgent,
		"events_sent", m.Counter.GetValue())

	delete(svc.consumers, id)
}

// GET+websocket /xrpc/com.atproto.sync.subscribeRepos
func (svc *Service) EventsHandler(c echo.Context) error {
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
					svc.log.Warn("failed to ping client", "err", err)
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
				svc.log.Warn("failed to read message from client", "err", err)
				cancel()
				return
			}
		}
	}()

	ident := c.RealIP() + "-" + c.Request().UserAgent()

	evts, cleanup, err := svc.events.Subscribe(ctx, ident, func(evt *stream.XRPCStreamEvent) bool { return true }, since)
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

	consumerID := svc.registerConsumer(&consumer)
	defer svc.cleanupConsumer(consumerID)

	logger := svc.log.With(
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
func (s *Service) domainIsBanned(ctx context.Context, host string) (bool, error) {
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

func (s *Service) findDomainBan(ctx context.Context, host string) (bool, error) {
	var db slurper.DomainBan
	if err := s.db.Find(&db, "domain = ?", host).Error; err != nil {
		return false, err
	}

	if db.ID == 0 {
		return false, nil
	}

	return true, nil
}

var ErrNotFound = errors.New("not found")

func (svc *Service) DidToUid(ctx context.Context, did string) (models.Uid, error) {
	xu, err := svc.lookupUserByDid(ctx, did)
	if err != nil {
		return 0, err
	}
	if xu == nil {
		return 0, ErrNotFound
	}
	return xu.ID, nil
}

func (svc *Service) lookupUserByDid(ctx context.Context, did string) (*slurper.Account, error) {
	ctx, span := tracer.Start(ctx, "lookupUserByDid")
	defer span.End()

	cu, ok := svc.userCache.Get(did)
	if ok {
		return cu, nil
	}

	var u slurper.Account
	if err := svc.db.Find(&u, "did = ?", did).Error; err != nil {
		return nil, err
	}

	if u.ID == 0 {
		return nil, gorm.ErrRecordNotFound
	}

	svc.userCache.Add(did, &u)

	return &u, nil
}

func (svc *Service) lookupUserByUID(ctx context.Context, uid models.Uid) (*slurper.Account, error) {
	ctx, span := tracer.Start(ctx, "lookupUserByUID")
	defer span.End()

	var u slurper.Account
	if err := svc.db.Find(&u, "id = ?", uid).Error; err != nil {
		return nil, err
	}

	if u.ID == 0 {
		return nil, gorm.ErrRecordNotFound
	}

	return &u, nil
}

// handleFedEvent() is the callback passed to Slurper called from Slurper.handleConnection()
func (svc *Service) handleFedEvent(ctx context.Context, host *slurper.PDS, env *stream.XRPCStreamEvent) error {
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
		return svc.handleCommit(ctx, host, env.RepoCommit)
	case env.RepoSync != nil:
		repoSyncReceivedCounter.WithLabelValues(host.Host).Add(1)
		return svc.handleSync(ctx, host, env.RepoSync)
	case env.RepoHandle != nil:
		eventsWarningsCounter.WithLabelValues(host.Host, "handle").Add(1)
		// TODO: rate limit warnings per PDS before we (temporarily?) block them
		return nil
	case env.RepoIdentity != nil:
		svc.log.Info("relay got identity event", "did", env.RepoIdentity.Did)
		// Flush any cached DID documents for this user
		svc.purgeDidCache(ctx, env.RepoIdentity.Did)

		// Refetch the DID doc and update our cached keys and handle etc.
		account, err := svc.syncPDSAccount(ctx, env.RepoIdentity.Did, host, nil)
		if err != nil {
			return err
		}

		// Broadcast the identity event to all consumers
		err = svc.events.AddEvent(ctx, &stream.XRPCStreamEvent{
			RepoIdentity: &comatproto.SyncSubscribeRepos_Identity{
				Did:    env.RepoIdentity.Did,
				Seq:    env.RepoIdentity.Seq,
				Time:   env.RepoIdentity.Time,
				Handle: env.RepoIdentity.Handle,
			},
			PrivUid: account.ID,
		})
		if err != nil {
			svc.log.Error("failed to broadcast Identity event", "error", err, "did", env.RepoIdentity.Did)
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
		svc.log.Info("relay got account event", "did", env.RepoAccount.Did)

		if !env.RepoAccount.Active && env.RepoAccount.Status == nil {
			accountVerifyWarnings.WithLabelValues(host.Host, "nostat").Inc()
			return nil
		}

		// Flush any cached DID documents for this user
		svc.purgeDidCache(ctx, env.RepoAccount.Did)

		// Refetch the DID doc to make sure the PDS is still authoritative
		account, err := svc.syncPDSAccount(ctx, env.RepoAccount.Did, host, nil)
		if err != nil {
			span.RecordError(err)
			return err
		}

		// Check if the PDS is still authoritative
		// if not we don't want to be propagating this account event
		if account.GetPDS() != host.ID {
			svc.log.Error("account event from non-authoritative pds",
				"seq", env.RepoAccount.Seq,
				"did", env.RepoAccount.Did,
				"event_from", host.Host,
				"did_doc_declared_pds", account.GetPDS(),
				"account_evt", env.RepoAccount,
			)
			return fmt.Errorf("event from non-authoritative pds")
		}

		// Process the account status change
		repoStatus := slurper.AccountStatusActive
		if !env.RepoAccount.Active && env.RepoAccount.Status != nil {
			repoStatus = *env.RepoAccount.Status
		}

		account.SetUpstreamStatus(repoStatus)
		err = svc.db.Save(account).Error
		if err != nil {
			span.RecordError(err)
			return fmt.Errorf("failed to update account status: %w", err)
		}

		shouldBeActive := env.RepoAccount.Active
		status := env.RepoAccount.Status

		// override with local status
		if account.GetTakenDown() {
			shouldBeActive = false
			status = &slurper.AccountStatusTakendown
		}

		// Broadcast the account event to all consumers
		err = svc.events.AddEvent(ctx, &stream.XRPCStreamEvent{
			RepoAccount: &comatproto.SyncSubscribeRepos_Account{
				Active: shouldBeActive,
				Did:    env.RepoAccount.Did,
				Seq:    env.RepoAccount.Seq,
				Status: status,
				Time:   env.RepoAccount.Time,
			},
			PrivUid: account.ID,
		})
		if err != nil {
			svc.log.Error("failed to broadcast Account event", "error", err, "did", env.RepoAccount.Did)
			return fmt.Errorf("failed to broadcast Account event: %w", err)
		}

		return nil
	case env.RepoMigrate != nil:
		eventsWarningsCounter.WithLabelValues(host.Host, "migrate").Add(1)
		// TODO: rate limit warnings per PDS before we (temporarily?) block them
		return nil
	case env.RepoTombstone != nil:
		eventsWarningsCounter.WithLabelValues(host.Host, "tombstone").Add(1)
		// TODO: rate limit warnings per PDS before we (temporarily?) block them
		return nil
	default:
		return fmt.Errorf("invalid fed event")
	}
}

func (svc *Service) newUser(ctx context.Context, host *slurper.PDS, did string) (*slurper.Account, error) {
	newUsersDiscovered.Inc()
	start := time.Now()
	account, err := svc.syncPDSAccount(ctx, did, host, nil)
	newUserDiscoveryDuration.Observe(time.Since(start).Seconds())
	if err != nil {
		repoCommitsResultCounter.WithLabelValues(host.Host, "uerr").Inc()
		return nil, fmt.Errorf("fed event create external user: %w", err)
	}
	return account, nil
}

var ErrCommitNoUser = errors.New("commit no user")

func (svc *Service) handleCommit(ctx context.Context, host *slurper.PDS, evt *comatproto.SyncSubscribeRepos_Commit) error {
	svc.log.Debug("relay got repo append event", "seq", evt.Seq, "pdsHost", host.Host, "repo", evt.Repo)

	account, err := svc.lookupUserByDid(ctx, evt.Repo)
	if err != nil {
		if !errors.Is(err, gorm.ErrRecordNotFound) {
			repoCommitsResultCounter.WithLabelValues(host.Host, "nou").Inc()
			return fmt.Errorf("looking up event user: %w", err)
		}

		account, err = svc.newUser(ctx, host, evt.Repo)
		if err != nil {
			repoCommitsResultCounter.WithLabelValues(host.Host, "nuerr").Inc()
			return err
		}
	}
	if account == nil {
		repoCommitsResultCounter.WithLabelValues(host.Host, "nou2").Inc()
		return ErrCommitNoUser
	}

	ustatus := account.GetUpstreamStatus()

	if account.GetTakenDown() || ustatus == slurper.AccountStatusTakendown {
		svc.log.Debug("dropping commit event from taken down user", "did", evt.Repo, "seq", evt.Seq, "pdsHost", host.Host)
		repoCommitsResultCounter.WithLabelValues(host.Host, "tdu").Inc()
		return nil
	}

	if ustatus == slurper.AccountStatusSuspended {
		svc.log.Debug("dropping commit event from suspended user", "did", evt.Repo, "seq", evt.Seq, "pdsHost", host.Host)
		repoCommitsResultCounter.WithLabelValues(host.Host, "susu").Inc()
		return nil
	}

	if ustatus == slurper.AccountStatusDeactivated {
		svc.log.Debug("dropping commit event from deactivated user", "did", evt.Repo, "seq", evt.Seq, "pdsHost", host.Host)
		repoCommitsResultCounter.WithLabelValues(host.Host, "du").Inc()
		return nil
	}

	if evt.Rebase {
		repoCommitsResultCounter.WithLabelValues(host.Host, "rebase").Inc()
		return fmt.Errorf("rebase was true in event seq:%d,host:%s", evt.Seq, host.Host)
	}

	accountPDSId := account.GetPDS()
	if host.ID != accountPDSId && accountPDSId != 0 {
		svc.log.Warn("received event for repo from different pds than expected", "repo", evt.Repo, "expPds", accountPDSId, "gotPds", host.Host)
		// Flush any cached DID documents for this user
		svc.purgeDidCache(ctx, evt.Repo)

		account, err = svc.syncPDSAccount(ctx, evt.Repo, host, account)
		if err != nil {
			repoCommitsResultCounter.WithLabelValues(host.Host, "uerr2").Inc()
			return err
		}

		if account.GetPDS() != host.ID {
			repoCommitsResultCounter.WithLabelValues(host.Host, "noauth").Inc()
			return fmt.Errorf("event from non-authoritative pds")
		}
	}

	var prevState slurper.AccountPreviousState
	err = svc.db.First(&prevState, account.ID).Error
	prevP := &prevState
	if errors.Is(err, gorm.ErrRecordNotFound) {
		prevP = nil
	} else if err != nil {
		svc.log.Error("failed to get previous root", "err", err)
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
	newRootCid, err := svc.validator.HandleCommit(ctx, host, account, evt, prevP)
	if err != nil {
		svc.inductionTraceLog.Error("commit bad", "seq", evt.Seq, "pseq", dbPrevSeqStr, "pdsHost", host.Host, "repo", evt.Repo, "prev", evtPrevDataStr, "dbprev", dbPrevRootStr, "err", err)
		svc.log.Warn("failed handling event", "err", err, "pdsHost", host.Host, "seq", evt.Seq, "repo", account.Did, "commit", evt.Commit.String())
		repoCommitsResultCounter.WithLabelValues(host.Host, "err").Inc()
		return fmt.Errorf("handle user event failed: %w", err)
	} else {
		// store now verified new repo state
		err = svc.upsertPrevState(account.ID, newRootCid, evt.Rev, evt.Seq)
		if err != nil {
			return fmt.Errorf("failed to set previous root uid=%d: %w", account.ID, err)
		}
	}

	repoCommitsResultCounter.WithLabelValues(host.Host, "ok").Inc()

	// Broadcast the identity event to all consumers
	commitCopy := *evt
	err = svc.events.AddEvent(ctx, &stream.XRPCStreamEvent{
		RepoCommit: &commitCopy,
		PrivUid:    account.GetUid(),
	})
	if err != nil {
		svc.log.Error("failed to broadcast commit event", "error", err, "did", evt.Repo)
		return fmt.Errorf("failed to broadcast commit event: %w", err)
	}

	return nil
}

// handleSync processes #sync messages
func (svc *Service) handleSync(ctx context.Context, host *slurper.PDS, evt *comatproto.SyncSubscribeRepos_Sync) error {
	account, err := svc.lookupUserByDid(ctx, evt.Did)
	if err != nil {
		if !errors.Is(err, gorm.ErrRecordNotFound) {
			repoCommitsResultCounter.WithLabelValues(host.Host, "nou").Inc()
			return fmt.Errorf("looking up event user: %w", err)
		}

		account, err = svc.newUser(ctx, host, evt.Did)
	}
	if err != nil {
		return fmt.Errorf("could not get user for did %#v: %w", evt.Did, err)
	}

	newRootCid, err := svc.validator.HandleSync(ctx, host, evt)
	if err != nil {
		return err
	}
	err = svc.upsertPrevState(account.ID, newRootCid, evt.Rev, evt.Seq)
	if err != nil {
		return fmt.Errorf("could not sync set previous state uid=%d: %w", account.ID, err)
	}

	// Broadcast the sync event to all consumers
	evtCopy := *evt
	err = svc.events.AddEvent(ctx, &stream.XRPCStreamEvent{
		RepoSync: &evtCopy,
	})
	if err != nil {
		svc.log.Error("failed to broadcast sync event", "error", err, "did", evt.Did)
		return fmt.Errorf("failed to broadcast sync event: %w", err)
	}

	return nil
}

func (svc *Service) upsertPrevState(accountID models.Uid, newRootCid *cid.Cid, rev string, seq int64) error {
	cidBytes := newRootCid.Bytes()
	return svc.db.Exec(
		"INSERT INTO account_previous_states (uid, cid, rev, seq) VALUES (?, ?, ?, ?) ON CONFLICT (uid) DO UPDATE SET cid = EXCLUDED.cid, rev = EXCLUDED.rev, seq = EXCLUDED.seq",
		accountID, cidBytes, rev, seq,
	).Error
}

func (svc *Service) purgeDidCache(ctx context.Context, did string) {
	ati, err := syntax.ParseAtIdentifier(did)
	if err != nil {
		return
	}
	_ = svc.dir.Purge(ctx, *ati)
}

// syncPDSAccount ensures that a DID has an account record in the database attached to a PDS record in the database
// Some fields may be updated if needed.
// did is the user
// host is the PDS we received this from, not necessarily the canonical PDS in the DID document
// cachedAccount is (optionally) the account that we have already looked up from cache or database
func (svc *Service) syncPDSAccount(ctx context.Context, did string, host *slurper.PDS, cachedAccount *slurper.Account) (*slurper.Account, error) {
	ctx, span := tracer.Start(ctx, "syncPDSAccount")
	defer span.End()

	externalUserCreationAttempts.Inc()

	svc.log.Debug("create external user", "did", did)

	// lookup identity so that we know a DID's canonical source PDS
	pdid, err := syntax.ParseDID(did)
	if err != nil {
		return nil, fmt.Errorf("bad did %#v, %w", did, err)
	}
	ident, err := svc.dir.LookupDID(ctx, pdid)
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

	// is the canonical PDS banned?
	ban, err := svc.domainIsBanned(ctx, durl.Host)
	if err != nil {
		return nil, fmt.Errorf("failed to check pds ban status: %w", err)
	}
	if ban {
		return nil, fmt.Errorf("cannot create user on pds with banned domain")
	}

	if strings.HasPrefix(durl.Host, "localhost:") {
		durl.Scheme = "http"
	}

	var canonicalHost *slurper.PDS
	if host.Host == durl.Host {
		// we got the message from the canonical PDS, convenient!
		canonicalHost = host
	} else {
		// we got the message from an intermediate relay
		// check our db for info on canonical PDS
		var peering slurper.PDS
		if err := svc.db.Find(&peering, "host = ?", durl.Host).Error; err != nil {
			svc.log.Error("failed to find pds", "host", durl.Host)
			return nil, err
		}
		canonicalHost = &peering
	}

	if canonicalHost.Blocked {
		return nil, fmt.Errorf("refusing to create user with blocked PDS")
	}

	if canonicalHost.ID == 0 {
		// we got an event from a non-canonical PDS (an intermediate relay)
		// a non-canonical PDS we haven't seen before; ping it to make sure it's real
		// TODO: what do we actually want to track about the source we immediately got this message from vs the canonical PDS?
		svc.log.Warn("pds discovered in new user flow", "pds", durl.String(), "did", did)

		// Do a trivial API request against the PDS to verify that it exists
		pclient := &xrpc.Client{Host: durl.String()}
		svc.config.ApplyPDSClientSettings(pclient)
		cfg, err := comatproto.ServerDescribeServer(ctx, pclient)
		if err != nil {
			// TODO: failing this shouldn't halt our indexing
			return nil, fmt.Errorf("failed to check unrecognized pds: %w", err)
		}

		// since handles can be anything, checking against this list doesn't matter...
		_ = cfg

		// could check other things, a valid response is good enough for now
		canonicalHost.Host = durl.Host
		canonicalHost.SSL = (durl.Scheme == "https")
		canonicalHost.RateLimit = float64(svc.slurper.DefaultPerSecondLimit)
		canonicalHost.HourlyEventLimit = svc.slurper.DefaultPerHourLimit
		canonicalHost.DailyEventLimit = svc.slurper.DefaultPerDayLimit
		canonicalHost.RepoLimit = svc.slurper.DefaultRepoLimit

		if svc.ssl && !canonicalHost.SSL {
			return nil, fmt.Errorf("did references non-ssl PDS, this is disallowed in prod: %q %q", did, pdsService.URL)
		}

		if err := svc.db.Create(&canonicalHost).Error; err != nil {
			return nil, err
		}
	}

	if canonicalHost.ID == 0 {
		panic("somehow failed to create a pds entry?")
	}

	if canonicalHost.RepoCount >= canonicalHost.RepoLimit {
		// TODO: soft-limit / hard-limit ? create account in 'throttled' state, unless there are _really_ too many accounts
		return nil, fmt.Errorf("refusing to create user on PDS at max repo limit for pds %q", canonicalHost.Host)
	}

	// this lock just governs the lower half of this function
	svc.extUserLk.Lock()
	defer svc.extUserLk.Unlock()

	if cachedAccount == nil {
		cachedAccount, err = svc.lookupUserByDid(ctx, did)
	}
	if errors.Is(err, ErrNotFound) || errors.Is(err, gorm.ErrRecordNotFound) {
		err = nil
	}
	if err != nil {
		return nil, err
	}
	if cachedAccount != nil {
		caPDS := cachedAccount.GetPDS()
		if caPDS != canonicalHost.ID {
			// Account is now on a different PDS, update
			err = svc.db.Transaction(func(tx *gorm.DB) error {
				if caPDS != 0 {
					// decrement prior PDS's account count
					tx.Model(&slurper.PDS{}).Where("id = ?", caPDS).Update("repo_count", gorm.Expr("repo_count - 1"))
				}
				// update user's PDS ID
				res := tx.Model(slurper.Account{}).Where("id = ?", cachedAccount.ID).Update("pds", canonicalHost.ID)
				if res.Error != nil {
					return fmt.Errorf("failed to update users pds: %w", res.Error)
				}
				// increment new PDS's account count
				res = tx.Model(&slurper.PDS{}).Where("id = ? AND repo_count < repo_limit", canonicalHost.ID).Update("repo_count", gorm.Expr("repo_count + 1"))
				return nil
			})

			cachedAccount.SetPDS(canonicalHost.ID)
		}
		return cachedAccount, nil
	}

	newAccount := slurper.Account{
		Did: did,
		PDS: canonicalHost.ID,
	}

	err = svc.db.Transaction(func(tx *gorm.DB) error {
		res := tx.Model(&slurper.PDS{}).Where("id = ? AND repo_count < repo_limit", canonicalHost.ID).Update("repo_count", gorm.Expr("repo_count + 1"))
		if res.Error != nil {
			return fmt.Errorf("failed to increment repo count for pds %q: %w", canonicalHost.Host, res.Error)
		}
		if terr := tx.Create(&newAccount).Error; terr != nil {
			svc.log.Error("failed to create user", "did", newAccount.Did, "err", terr)
			return fmt.Errorf("failed to create other pds user: %w", terr)
		}
		return nil
	})
	if err != nil {
		svc.log.Error("user create and pds inc err", "err", err)
		return nil, err
	}

	svc.userCache.Add(did, &newAccount)

	return &newAccount, nil
}

func (svc *Service) TakeDownRepo(ctx context.Context, did string) error {
	u, err := svc.lookupUserByDid(ctx, did)
	if err != nil {
		return err
	}

	if err := svc.db.Model(slurper.Account{}).Where("id = ?", u.ID).Update("taken_down", true).Error; err != nil {
		return err
	}
	u.SetTakenDown(true)

	if err := svc.events.TakeDownRepo(ctx, u.ID); err != nil {
		return err
	}

	return nil
}

func (svc *Service) ReverseTakedown(ctx context.Context, did string) error {
	u, err := svc.lookupUserByDid(ctx, did)
	if err != nil {
		return err
	}

	if err := svc.db.Model(slurper.Account{}).Where("id = ?", u.ID).Update("taken_down", false).Error; err != nil {
		return err
	}
	u.SetTakenDown(false)

	return nil
}

func (svc *Service) GetRepoRoot(ctx context.Context, user models.Uid) (cid.Cid, error) {
	var prevState slurper.AccountPreviousState
	err := svc.db.First(&prevState, user).Error
	if err == nil {
		return prevState.Cid.CID, nil
	} else if errors.Is(err, gorm.ErrRecordNotFound) {
		return cid.Cid{}, ErrUserStatusUnavailable
	} else {
		svc.log.Error("user db err", "err", err)
		return cid.Cid{}, fmt.Errorf("user prev db err, %w", err)
	}
}
