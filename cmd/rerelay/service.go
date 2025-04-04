package main

import (
	"context"
	"encoding/base64"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/bluesky-social/indigo/cmd/rerelay/relay"

	"github.com/labstack/echo/v4"
	"github.com/labstack/echo/v4/middleware"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"gorm.io/gorm"
)

type Service struct {
	db     *gorm.DB // XXX
	logger *slog.Logger
	relay  *relay.Relay
	config ServiceConfig

	crawlForwardClient http.Client
}

type ServiceConfig struct {
	// NextCrawlers gets forwarded POST /xrpc/com.atproto.sync.requestCrawl
	NextCrawlers []*url.URL

	// verified against Basic admin auth
	AdminPassword string

	// how long to wait for the requested server socket to become available for use
	ListenerBootTimeout time.Duration
}

func DefaultServiceConfig() *ServiceConfig {
	return &ServiceConfig{
		ListenerBootTimeout: 5 * time.Second,
	}
}

func NewService(db *gorm.DB, r *relay.Relay, config *ServiceConfig) (*Service, error) {

	if config == nil {
		config = DefaultServiceConfig()
	}

	svc := &Service{
		db:                 db,
		logger:             slog.Default().With("system", "relay"),
		relay:              r,
		config:             *config,
		crawlForwardClient: http.Client{},
	}
	svc.crawlForwardClient.Timeout = time.Second * 5

	return svc, nil
}

func (svc *Service) StartMetrics(listen string) error {
	http.Handle("/metrics", promhttp.Handler())
	return http.ListenAndServe(listen, nil)
}

func (svc *Service) StartAPI(bind string) error {
	var lc net.ListenConfig
	ctx, cancel := context.WithTimeout(context.Background(), svc.config.ListenerBootTimeout)
	defer cancel()

	li, err := lc.Listen(ctx, "tcp", bind)
	if err != nil {
		return err
	}
	return svc.startWithListener(li)
}

func (svc *Service) startWithListener(listen net.Listener) error {
	e := echo.New()
	e.HideBanner = true

	e.Use(middleware.CORSWithConfig(middleware.CORSConfig{
		AllowOrigins: []string{"*"},
		AllowHeaders: []string{echo.HeaderOrigin, echo.HeaderContentType, echo.HeaderAccept, echo.HeaderAuthorization},
	}))

	if !svc.relay.Config.SSL {
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
				svc.logger.Error("Failed to write http error", "err", err2)
			}
		default:
			sendHeader := true
			if ctx.Path() == "/xrpc/com.atproto.sync.subscribeRepos" {
				sendHeader = false
			}

			svc.logger.Warn("HANDLER ERROR: (%s) %s", ctx.Path(), err)

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
	e.GET("/", svc.HandleHomeMessage)
	e.GET("/_health", svc.HandleHealthCheck)
	e.GET("/xrpc/_health", svc.HandleHealthCheck)

	e.GET("/xrpc/com.atproto.sync.subscribeRepos", svc.HandleComAtprotoSyncSubscribeRepos)

	e.POST("/xrpc/com.atproto.sync.requestCrawl", svc.HandleComAtprotoSyncRequestCrawl)
	e.GET("/xrpc/com.atproto.sync.listRepos", svc.HandleComAtprotoSyncListRepos)
	e.GET("/xrpc/com.atproto.sync.getRepo", svc.HandleComAtprotoSyncGetRepo) // just returns 3xx redirect to source PDS
	e.GET("/xrpc/com.atproto.sync.getLatestCommit", svc.HandleComAtprotoSyncGetLatestCommit)

	/* XXX: disabled while refactoring
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
	*/

	// In order to support booting on random ports in tests, we need to tell the
	// Echo instance it's already got a port, and then use its StartServer
	// method to re-use that listener.
	e.Listener = listen
	srv := &http.Server{}
	// TODO: attach echo to Service, for shutdown?
	return e.StartServer(srv)
}

func (svc *Service) Shutdown() []error {
	var errs []error
	if err := svc.relay.Slurper.Shutdown(); err != nil {
		errs = append(errs, err)
	}

	if err := svc.relay.Events.Shutdown(context.TODO()); err != nil {
		errs = append(errs, err)
	}

	return errs
}

func (svc *Service) checkAdminAuth(next echo.HandlerFunc) echo.HandlerFunc {
	headerVal := "Basic " + base64.StdEncoding.EncodeToString([]byte("admin:"+svc.config.AdminPassword))
	return func(e echo.Context) error {
		if svc.config.AdminPassword != headerVal {
			return echo.ErrForbidden
		}
		return next(e)
	}
}
