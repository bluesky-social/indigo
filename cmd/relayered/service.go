package main

import (
	"context"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/bluesky-social/indigo/atproto/identity"
	"github.com/bluesky-social/indigo/cmd/relayered/relay"

	"github.com/labstack/echo/v4"
	"github.com/labstack/echo/v4/middleware"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"gorm.io/gorm"
)

// serverListenerBootTimeout is how long to wait for the requested server socket
// to become available for use. This is an arbitrary timeout that should be safe
// on any platform, but there's no great way to weave this timeout without
// adding another parameter to the (at time of writing) long signature of
// NewServer.
const serverListenerBootTimeout = 5 * time.Second

type Service struct {
	db    *gorm.DB // XXX
	relay *relay.Relay
	dir   identity.Directory

	// TODO: work on doing away with this flag in favor of more pluggable
	// pieces that abstract the need for explicit ssl checks
	ssl bool

	// nextCrawlers gets forwarded POST /xrpc/com.atproto.sync.requestCrawl
	nextCrawlers []*url.URL
	httpClient   http.Client

	log *slog.Logger

	config ServiceConfig
}

type ServiceConfig struct {
	// NextCrawlers gets forwarded POST /xrpc/com.atproto.sync.requestCrawl
	NextCrawlers []*url.URL

	// AdminToken checked against "Authorization: Bearer {}" header
	AdminToken string
}

func DefaultServiceConfig() *ServiceConfig {
	return &ServiceConfig{}
}

func NewService(db *gorm.DB, r *relay.Relay, dir identity.Directory, config *ServiceConfig) (*Service, error) {

	if config == nil {
		config = DefaultServiceConfig()
	}

	svc := &Service{
		db:    db,
		relay: r,
		dir:   dir,
		ssl:   r.Config.SSL,

		log: slog.Default().With("system", "relay"),

		config: *config,
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

	e.GET("/xrpc/com.atproto.sync.subscribeRepos", svc.relay.EventsHandler)

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
	errs := svc.relay.Slurper.Shutdown()

	if err := svc.relay.Events.Shutdown(context.TODO()); err != nil {
		errs = append(errs, err)
	}

	return errs
}

type HealthStatus struct {
	Status  string `json:"status"`
	Message string `json:"msg,omitempty"`
}

func (svc *Service) HandleHealthCheck(c echo.Context) error {
	if err := svc.relay.Healthcheck(); err != nil {
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
