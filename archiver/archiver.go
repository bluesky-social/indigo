package archiver

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/bluesky-social/indigo/bgs"
	"github.com/bluesky-social/indigo/carstore"
	"github.com/bluesky-social/indigo/did"
	"github.com/bluesky-social/indigo/events"
	"github.com/bluesky-social/indigo/models"
	"github.com/bluesky-social/indigo/repomgr"
	"github.com/bluesky-social/indigo/util/svcutil"
	lru "github.com/hashicorp/golang-lru/v2"
	ipld "github.com/ipfs/go-ipld-format"
	"github.com/labstack/echo/v4"
	"github.com/labstack/echo/v4/middleware"
	"github.com/labstack/gommon/log"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"gorm.io/gorm"
)

var tracer = otel.Tracer("archiver")

type Archiver struct {
	db      *gorm.DB
	slurper *bgs.Slurper
	didr    did.Resolver

	crawler *CrawlDispatcher

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
	consumers      map[uint64]*bgs.SocketConsumer

	// Management of Resyncs
	pdsResyncsLk sync.RWMutex
	pdsResyncs   map[uint]*bgs.PDSResync

	// User cache
	userCache *lru.Cache[string, *User]

	httpClient http.Client

	log *slog.Logger
}

type ArchiverConfig struct {
	SSL                  bool
	CompactInterval      time.Duration
	DefaultRepoLimit     int64
	ConcurrencyPerPDS    int64
	MaxQueuePerPDS       int64
	NumCompactionWorkers int

	// NextCrawlers gets forwarded POST /xrpc/com.atproto.sync.requestCrawl
	NextCrawlers []*url.URL
}

func DefaultArchiverConfig() *ArchiverConfig {
	return &ArchiverConfig{
		SSL:                  true,
		CompactInterval:      4 * time.Hour,
		DefaultRepoLimit:     100,
		ConcurrencyPerPDS:    100,
		MaxQueuePerPDS:       1_000,
		NumCompactionWorkers: 2,
	}
}

func NewArchiver(db *gorm.DB, repoman *repomgr.RepoManager, didr did.Resolver, rf *RepoFetcher, config *ArchiverConfig) (*Archiver, error) {
	if config == nil {
		config = DefaultArchiverConfig()
	}
	db.AutoMigrate(User{})
	db.AutoMigrate(AuthToken{})
	db.AutoMigrate(models.PDS{})
	db.AutoMigrate(models.DomainBan{})

	uc, _ := lru.New[string, *User](1_000_000)

	log := slog.Default().With("system", "archiver")

	c, err := NewCrawlDispatcher(rf, rf.MaxConcurrency, log)
	if err != nil {
		return nil, err
	}

	c.Run()

	arc := &Archiver{
		crawler: c,
		db:      db,

		repoman: repoman,
		didr:    didr,
		ssl:     config.SSL,

		consumersLk: sync.RWMutex{},
		consumers:   make(map[uint64]*bgs.SocketConsumer),

		pdsResyncs: make(map[uint]*bgs.PDSResync),

		userCache: uc,

		log: log,
	}

	slOpts := bgs.DefaultSlurperOptions()
	slOpts.SSL = config.SSL
	slOpts.DefaultRepoLimit = config.DefaultRepoLimit
	slOpts.ConcurrencyPerPDS = config.ConcurrencyPerPDS
	slOpts.MaxQueuePerPDS = config.MaxQueuePerPDS
	s, err := bgs.NewSlurper(db, arc.handleFedEvent, slOpts)
	if err != nil {
		return nil, err
	}

	arc.slurper = s

	if err := arc.slurper.RestartAll(); err != nil {
		return nil, err
	}

	arc.httpClient.Timeout = time.Second * 5

	return arc, nil

}

type User struct {
	gorm.Model
	ID  models.Uid `gorm:"primarykey;index:idx_user_id_active,where:taken_down = false AND tombstoned = false"`
	Did string     `gorm:"uniqueindex"`
	PDS uint

	// UpstreamStatus is the state of the user as reported by the upstream PDS
	UpstreamStatus string `gorm:"index"`

	lk sync.Mutex
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

func (s *Archiver) handleUserUpdate(ctx context.Context, did string) (*User, error) {
	ctx, span := tracer.Start(ctx, "handleUserUpdate")
	defer span.End()

	externalUserCreationAttempts.Inc()

	s.log.Debug("create external user", "did", did)
	doc, err := s.didr.GetDocument(ctx, did)
	if err != nil {
		return nil, fmt.Errorf("could not locate DID document for user (%s): %w", did, err)
	}

	if len(doc.Service) == 0 {
		return nil, fmt.Errorf("user %s had no services in did document", did)
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
		s.log.Error("failed to find pds", "host", durl.Host)
		return nil, err
	}

	if peering.ID == 0 {
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
				s.log.Error("failed to decrement repo count for pds", "err", err)
			}
		}
	}()

	if len(doc.AlsoKnownAs) == 0 {
		return nil, fmt.Errorf("user has no 'known as' field in their DID document")
	}

	s.log.Debug("creating external user", "did", did, "pds", peering.ID)

	s.extUserLk.Lock()
	defer s.extUserLk.Unlock()

	exu, err := s.LookupUserByDid(ctx, did)
	if err == nil {
		s.log.Debug("lost the race to create a new user", "did", did)
		if exu.PDS != peering.ID {
			// User is now on a different PDS, update
			if err := s.db.Model(User{}).Where("id = ?", exu.ID).Update("pds", peering.ID).Error; err != nil {
				return nil, fmt.Errorf("failed to update users pds: %w", err)
			}

			exu.PDS = peering.ID
		}

		return exu, nil
	}

	if !errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, err
	}

	u := User{
		Did: did,
		PDS: peering.ID,
	}

	if err := s.db.Create(&u).Error; err != nil {
		return nil, fmt.Errorf("failed to create other pds user: %w", err)
	}

	successfullyCreated = true

	return &u, nil
}

func (s *Archiver) handleFedEvent(ctx context.Context, host *models.PDS, env *events.XRPCStreamEvent) error {
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
		s.log.Debug("archiver got repo append event", "seq", evt.Seq, "pdsHost", host.Host, "repo", evt.Repo)

		st := time.Now()
		u, err := s.handleUserUpdate(ctx, evt.Repo)
		userLookupDuration.Observe(time.Since(st).Seconds())
		if err != nil {
			return fmt.Errorf("looking up event user: %w", err)
		}

		ustatus := u.GetUpstreamStatus()
		span.SetAttributes(attribute.String("upstream_status", ustatus))

		switch ustatus {
		case events.AccountStatusTakendown:
			s.log.Debug("dropping commit event from taken down user", "did", evt.Repo, "seq", evt.Seq, "pdsHost", host.Host)
			repoCommitsResultCounter.WithLabelValues(host.Host, "tdu").Inc()
			return nil
		case events.AccountStatusSuspended:
			s.log.Debug("dropping commit event from suspended user", "did", evt.Repo, "seq", evt.Seq, "pdsHost", host.Host)
			repoCommitsResultCounter.WithLabelValues(host.Host, "susu").Inc()
			return nil
		case events.AccountStatusDeactivated:
			s.log.Debug("dropping commit event from deactivated user", "did", evt.Repo, "seq", evt.Seq, "pdsHost", host.Host)
			repoCommitsResultCounter.WithLabelValues(host.Host, "du").Inc()
			return nil
		}

		if evt.Rebase {
			repoCommitsResultCounter.WithLabelValues(host.Host, "rebase").Inc()
			return fmt.Errorf("rebase was true in event seq:%d,host:%s", evt.Seq, host.Host)
		}

		if host.ID != u.PDS && u.PDS != 0 && !host.Trusted {
			s.log.Warn("received event for repo from different pds than expected", "repo", evt.Repo, "expPds", u.PDS, "gotPds", host.Host)
			// Flush any cached DID documents for this user
			s.didr.FlushCacheFor(env.RepoCommit.Repo)

			subj, err := s.handleUserUpdate(ctx, evt.Repo)
			if err != nil {
				repoCommitsResultCounter.WithLabelValues(host.Host, "uerr2").Inc()
				return err
			}

			if subj.PDS != host.ID {
				repoCommitsResultCounter.WithLabelValues(host.Host, "noauth").Inc()
				return fmt.Errorf("event from non-authoritative pds")
			}
		}

		// skip the fast path for rebases or if the user is already in the slow path
		if s.crawler.RepoInSlowPath(ctx, u.ID) {
			ai, err := s.LookupUser(ctx, u.ID)
			if err != nil {
				repoCommitsResultCounter.WithLabelValues(host.Host, "nou3").Inc()
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
			repoCommitsResultCounter.WithLabelValues(host.Host, "catchup").Inc()
			return s.crawler.AddToCatchupQueue(ctx, host, ai, evt)
		}

		if err := s.repoman.HandleExternalUserEvent(ctx, host.ID, u.ID, u.Did, evt.Since, evt.Rev, evt.Blocks, evt.Ops); err != nil {

			if errors.Is(err, carstore.ErrRepoBaseMismatch) || ipld.IsNotFound(err) {
				ai, lerr := s.LookupUser(ctx, u.ID)
				if lerr != nil {
					log.Warn("failed handling event, no user", "err", err, "pdsHost", host.Host, "seq", evt.Seq, "repo", u.Did, "commit", evt.Commit.String())
					repoCommitsResultCounter.WithLabelValues(host.Host, "nou4").Inc()
					return fmt.Errorf("failed to look up user %s (%d) (err case: %s): %w", u.Did, u.ID, err, lerr)
				}

				span.SetAttributes(attribute.Bool("catchup_queue", true))

				log.Info("failed handling event, catchup", "err", err, "pdsHost", host.Host, "seq", evt.Seq, "repo", u.Did, "commit", evt.Commit.String())
				repoCommitsResultCounter.WithLabelValues(host.Host, "catchup2").Inc()
				return s.crawler.AddToCatchupQueue(ctx, host, ai, evt)
			}

			log.Warn("failed handling event", "err", err, "pdsHost", host.Host, "seq", evt.Seq, "repo", u.Did, "commit", evt.Commit.String())
			repoCommitsResultCounter.WithLabelValues(host.Host, "err").Inc()
			return fmt.Errorf("handle user event failed: %w", err)
		}

		repoCommitsResultCounter.WithLabelValues(host.Host, "ok").Inc()
		return nil
	case env.RepoIdentity != nil:
		s.log.Info("archiver got identity event", "did", env.RepoIdentity.Did)
		// Flush any cached DID documents for this user
		s.didr.FlushCacheFor(env.RepoIdentity.Did)

		// Refetch the DID doc and update our cached keys and handle etc.
		_, err := s.handleUserUpdate(ctx, env.RepoIdentity.Did)
		if err != nil {
			return err
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

		s.log.Info("archiver got account event", "did", env.RepoAccount.Did)
		// Flush any cached DID documents for this user
		s.didr.FlushCacheFor(env.RepoAccount.Did)

		// Refetch the DID doc to make sure the PDS is still authoritative
		ai, err := s.handleUserUpdate(ctx, env.RepoAccount.Did)
		if err != nil {
			span.RecordError(err)
			return err
		}

		// Check if the PDS is still authoritative
		// if not we don't want to be propagating this account event
		if ai.PDS != host.ID {
			s.log.Error("account event from non-authoritative pds",
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

		err = s.UpdateAccountStatus(ctx, env.RepoAccount.Did, repoStatus)
		if err != nil {
			span.RecordError(err)
			return fmt.Errorf("failed to update account status: %w", err)
		}

		return nil
	default:
		return fmt.Errorf("invalid fed event")
	}
}

func (s *Archiver) UpdateAccountStatus(ctx context.Context, did string, status string) error {
	ctx, span := tracer.Start(ctx, "UpdateAccountStatus")
	defer span.End()

	span.SetAttributes(
		attribute.String("did", did),
		attribute.String("status", status),
	)

	u, err := s.lookupUserByDid(ctx, did)
	if err != nil {
		return err
	}

	switch status {
	case events.AccountStatusActive:
		// Unset the PDS-specific status flags
		if err := s.db.Model(User{}).Where("id = ?", u.ID).Update("upstream_status", events.AccountStatusActive).Error; err != nil {
			return fmt.Errorf("failed to set user active status: %w", err)
		}
		u.SetUpstreamStatus(events.AccountStatusActive)
	case events.AccountStatusDeactivated:
		if err := s.db.Model(User{}).Where("id = ?", u.ID).Update("upstream_status", events.AccountStatusDeactivated).Error; err != nil {
			return fmt.Errorf("failed to set user deactivation status: %w", err)
		}
		u.SetUpstreamStatus(events.AccountStatusDeactivated)
	case events.AccountStatusSuspended:
		if err := s.db.Model(User{}).Where("id = ?", u.ID).Update("upstream_status", events.AccountStatusSuspended).Error; err != nil {
			return fmt.Errorf("failed to set user suspension status: %w", err)
		}
		u.SetUpstreamStatus(events.AccountStatusSuspended)
	case events.AccountStatusTakendown:
		if err := s.db.Model(User{}).Where("id = ?", u.ID).Update("upstream_status", events.AccountStatusTakendown).Error; err != nil {
			return fmt.Errorf("failed to set user taken down status: %w", err)
		}
		u.SetUpstreamStatus(events.AccountStatusTakendown)

		if err := s.db.Model(&models.ActorInfo{}).Where("uid = ?", u.ID).UpdateColumns(map[string]any{
			"handle": nil,
		}).Error; err != nil {
			return err
		}
	case events.AccountStatusDeleted:
		if err := s.db.Model(&User{}).Where("id = ?", u.ID).UpdateColumns(map[string]any{
			"tombstoned":      true,
			"handle":          nil,
			"upstream_status": events.AccountStatusDeleted,
		}).Error; err != nil {
			return err
		}
		u.SetUpstreamStatus(events.AccountStatusDeleted)

		if err := s.db.Model(&models.ActorInfo{}).Where("uid = ?", u.ID).UpdateColumns(map[string]any{
			"handle": nil,
		}).Error; err != nil {
			return err
		}

		// delete data from carstore
		if err := s.repoman.TakeDownRepo(ctx, u.ID); err != nil {
			// don't let a failure here prevent us from propagating this event
			s.log.Error("failed to delete user data from carstore", "err", err)
		}
	}

	return nil
}

func (s *Archiver) lookupUserByDid(ctx context.Context, did string) (*User, error) {
	ctx, span := tracer.Start(ctx, "lookupUserByDid")
	defer span.End()

	cu, ok := s.userCache.Get(did)
	if ok {
		return cu, nil
	}

	var u User
	if err := s.db.Find(&u, "did = ?", did).Error; err != nil {
		return nil, err
	}

	if u.ID == 0 {
		return nil, gorm.ErrRecordNotFound
	}

	s.userCache.Add(did, &u)

	return &u, nil
}

func (s *Archiver) lookupUserByUID(ctx context.Context, uid models.Uid) (*User, error) {
	ctx, span := tracer.Start(ctx, "lookupUserByUID")
	defer span.End()

	var u User
	if err := s.db.Find(&u, "id = ?", uid).Error; err != nil {
		return nil, err
	}

	if u.ID == 0 {
		return nil, gorm.ErrRecordNotFound
	}

	return &u, nil
}

type AuthToken struct {
	gorm.Model
	Token string `gorm:"index"`
}

func (s *Archiver) lookupAdminToken(tok string) (bool, error) {
	var at AuthToken
	if err := s.db.Find(&at, "token = ?", tok).Error; err != nil {
		return false, err
	}

	if at.ID == 0 {
		return false, nil
	}

	return true, nil
}

func (s *Archiver) CreateAdminToken(tok string) error {
	exists, err := s.lookupAdminToken(tok)
	if err != nil {
		return err
	}

	if exists {
		return nil
	}

	return s.db.Create(&AuthToken{
		Token: tok,
	}).Error
}

func (s *Archiver) StartMetrics(listen string) error {
	http.Handle("/metrics", promhttp.Handler())
	return http.ListenAndServe(listen, nil)
}

const serverListenerBootTimeout = 5 * time.Second

func (s *Archiver) Start(addr string) error {
	var lc net.ListenConfig
	ctx, cancel := context.WithTimeout(context.Background(), serverListenerBootTimeout)
	defer cancel()

	li, err := lc.Listen(ctx, "tcp", addr)
	if err != nil {
		return err
	}
	return s.StartWithListener(li)
}

func (s *Archiver) StartWithListener(listen net.Listener) error {
	e := echo.New()
	e.HideBanner = true

	e.Use(middleware.CORSWithConfig(middleware.CORSConfig{
		AllowOrigins: []string{"*"},
		AllowHeaders: []string{echo.HeaderOrigin, echo.HeaderContentType, echo.HeaderAccept, echo.HeaderAuthorization},
	}))

	if !s.ssl {
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

	e.Use(svcutil.MetricsMiddleware)

	e.HTTPErrorHandler = func(err error, ctx echo.Context) {
		switch err := err.(type) {
		case *echo.HTTPError:
			if err2 := ctx.JSON(err.Code, map[string]any{
				"error": err.Message,
			}); err2 != nil {
				s.log.Error("Failed to write http error", "err", err2)
			}
		default:
			sendHeader := true
			if ctx.Path() == "/xrpc/com.atproto.sync.subscribeRepos" {
				sendHeader = false
			}

			s.log.Warn("HANDLER ERROR: (%s) %s", ctx.Path(), err)

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

	e.GET("/xrpc/com.atproto.sync.getRepo", s.HandleComAtprotoSyncGetRepo)
	//e.GET("/xrpc/com.atproto.sync.listRepos", s.HandleComAtprotoSyncListRepos)
	//e.GET("/xrpc/com.atproto.sync.getLatestCommit", s.HandleComAtprotoSyncGetLatestCommit)
	e.GET("/xrpc/_health", s.HandleHealthCheck)
	e.GET("/_health", s.HandleHealthCheck)
	e.GET("/", s.HandleHomeMessage)

	/*
		admin := e.Group("/admin", s.checkAdminAuth)

		// Slurper-related Admin API
		admin.GET("/subs/getUpstreamConns", s.handleAdminGetUpstreamConns)
		admin.GET("/subs/getEnabled", s.handleAdminGetSubsEnabled)
		admin.GET("/subs/perDayLimit", s.handleAdminGetNewPDSPerDayRateLimit)
		admin.POST("/subs/setEnabled", s.handleAdminSetSubsEnabled)
		admin.POST("/subs/killUpstream", s.handleAdminKillUpstreamConn)
		admin.POST("/subs/setPerDayLimit", s.handleAdminSetNewPDSPerDayRateLimit)

		// Domain-related Admin API
		admin.GET("/subs/listDomainBans", s.handleAdminListDomainBans)
		admin.POST("/subs/banDomain", s.handleAdminBanDomain)
		admin.POST("/subs/unbanDomain", s.handleAdminUnbanDomain)

		// Repo-related Admin API
		admin.POST("/repo/takeDown", s.handleAdminTakeDownRepo)
		admin.POST("/repo/reverseTakedown", s.handleAdminReverseTakedown)
		admin.GET("/repo/takedowns", s.handleAdminListRepoTakeDowns)
		admin.POST("/repo/compact", s.handleAdminCompactRepo)
		admin.POST("/repo/compactAll", s.handleAdminCompactAllRepos)
		admin.POST("/repo/reset", s.handleAdminResetRepo)
		admin.POST("/repo/verify", s.handleAdminVerifyRepo)

		// PDS-related Admin API
		admin.POST("/pds/requestCrawl", s.handleAdminRequestCrawl)
		admin.GET("/pds/list", s.handleListPDSs)
		admin.POST("/pds/resync", s.handleAdminPostResyncPDS)
		admin.GET("/pds/resync", s.handleAdminGetResyncPDS)
		admin.POST("/pds/changeLimits", s.handleAdminChangePDSRateLimits)
		admin.POST("/pds/block", s.handleBlockPDS)
		admin.POST("/pds/unblock", s.handleUnblockPDS)
		admin.POST("/pds/addTrustedDomain", s.handleAdminAddTrustedDomain)

		// Consumer-related Admin API
		admin.GET("/consumers/list", s.handleAdminListConsumers)
	*/

	// In order to support booting on random ports in tests, we need to tell the
	// Echo instance it's already got a port, and then use its StartServer
	// method to re-use that listener.
	e.Listener = listen
	srv := &http.Server{}
	return e.StartServer(srv)
}

func (s *Archiver) Shutdown() []error {
	errs := s.slurper.Shutdown()

	if s.crawler != nil {
		s.crawler.Shutdown()
	}

	return errs
}
