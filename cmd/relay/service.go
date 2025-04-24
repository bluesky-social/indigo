package main

import (
	"context"
	"crypto/subtle"
	"encoding/base64"
	"log/slog"
	"net"
	"net/http"
	"strings"
	"time"

	"github.com/bluesky-social/indigo/cmd/relay/relay"
	"github.com/bluesky-social/indigo/util/svcutil"

	"github.com/labstack/echo/v4"
	"github.com/labstack/echo/v4/middleware"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

type Service struct {
	logger *slog.Logger
	relay  *relay.Relay
	config ServiceConfig

	crawlForwardClient http.Client
}

type ServiceConfig struct {
	// list of hosts which get forwarded admin state changes (takedowns, etc)
	SiblingRelayHosts []string

	// verified against Basic admin auth. multiple passwords are allowed server-side, to make secret rotations operationally easier
	AdminPasswords []string

	// how long to wait for the requested server socket to become available for use
	ListenerBootTimeout time.Duration

	// if true, don't process public (unauthenticated) requestCrawl
	DisableRequestCrawl bool

	// if true, allows non-SSL hosts to be added via public requestCrawl
	AllowInsecureHosts bool
}

func DefaultServiceConfig() *ServiceConfig {
	return &ServiceConfig{
		ListenerBootTimeout: 5 * time.Second,
	}
}

func NewService(r *relay.Relay, config *ServiceConfig) (*Service, error) {

	if config == nil {
		config = DefaultServiceConfig()
	}

	svc := &Service{
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
	e.Use(middleware.LoggerWithConfig(middleware.DefaultLoggerConfig))

	// React uses a virtual router, so we need to serve the index.html for all
	// routes that aren't otherwise handled or in the /assets directory.
	e.File("/dash", "public/index.html")
	e.File("/dash/*", "public/index.html")
	e.Static("/assets", "public/assets")

	e.Use(svcutil.MetricsMiddleware)

	e.HTTPErrorHandler = func(err error, c echo.Context) {
		switch err := err.(type) {
		case *echo.HTTPError:
			if err2 := c.JSON(err.Code, map[string]any{
				"error": err.Message,
			}); err2 != nil {
				svc.logger.Error("Failed to write http error", "err", err2)
			}
		default:
			sendHeader := true
			if c.Path() == "/xrpc/com.atproto.sync.subscribeRepos" {
				sendHeader = false
			}

			svc.logger.Error("API handler error", "path", c.Path(), "err", err)

			if strings.HasPrefix(c.Path(), "/admin/") {
				_ = c.JSON(http.StatusInternalServerError, map[string]any{
					"error": err.Error(),
				})
				return
			}

			if sendHeader {
				c.Response().WriteHeader(http.StatusInternalServerError)
			}
		}
	}

	// TODO: this API is temporary until we formalize what we want here
	e.GET("/", svc.HandleHomeMessage)
	e.GET("/_health", svc.HandleHealthCheck)
	e.GET("/xrpc/_health", svc.HandleHealthCheck)

	e.GET("/xrpc/com.atproto.sync.subscribeRepos", svc.HandleComAtprotoSyncSubscribeRepos)
	e.POST("/xrpc/com.atproto.sync.requestCrawl", svc.HandleComAtprotoSyncRequestCrawl)
	e.GET("/xrpc/com.atproto.sync.listHosts", svc.HandleComAtprotoSyncListHosts)
	e.GET("/xrpc/com.atproto.sync.getHostStatus", svc.HandleComAtprotoSyncGetHostStatus)
	e.GET("/xrpc/com.atproto.sync.listRepos", svc.HandleComAtprotoSyncListRepos)
	e.GET("/xrpc/com.atproto.sync.getRepo", svc.HandleComAtprotoSyncGetRepo) // just returns 3xx redirect to source PDS
	e.GET("/xrpc/com.atproto.sync.getRepoStatus", svc.HandleComAtprotoSyncGetRepoStatus)
	e.GET("/xrpc/com.atproto.sync.getLatestCommit", svc.HandleComAtprotoSyncGetLatestCommit)

	admin := e.Group("/admin", svc.checkAdminAuth)

	// Slurper-related Admin API
	admin.GET("/subs/getUpstreamConns", svc.handleAdminGetUpstreamConns)
	admin.POST("/subs/killUpstream", svc.handleAdminKillUpstreamConn)
	admin.GET("/subs/getEnabled", svc.handleAdminGetSubsEnabled)
	admin.POST("/subs/setEnabled", svc.handleAdminSetSubsEnabled)
	admin.GET("/subs/perDayLimit", svc.handleAdminGetNewHostPerDayRateLimit)
	admin.POST("/subs/setPerDayLimit", svc.handleAdminSetNewHostPerDayRateLimit)

	// Domain-related Admin API
	admin.GET("/subs/listDomainBans", svc.handleAdminListDomainBans)
	admin.POST("/subs/banDomain", svc.handleAdminBanDomain)
	admin.POST("/subs/unbanDomain", svc.handleAdminUnbanDomain)

	// Repo-related Admin API
	admin.GET("/repo/takedowns", svc.handleAdminListRepoTakeDowns) // NOTE: unused
	admin.POST("/repo/takeDown", svc.handleAdminTakeDownRepo)
	admin.POST("/repo/reverseTakedown", svc.handleAdminReverseTakedown)

	// Host-related Admin API
	admin.GET("/pds/list", svc.handleListHosts)
	admin.POST("/pds/requestCrawl", svc.handleAdminRequestCrawl)
	admin.POST("/pds/changeLimits", svc.handleAdminChangeHostRateLimits)
	admin.POST("/pds/block", svc.handleBlockHost)
	admin.POST("/pds/unblock", svc.handleUnblockHost)
	// removed: admin.POST("/pds/addTrustedDomain", svc.handleAdminAddTrustedDomain)

	// Consumer-related Admin API
	admin.GET("/consumers/list", svc.handleAdminListConsumers)

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

	// pre-compute valid HTTP auth headers based on the set of
	validAuthHeaders := []string{}
	for _, pw := range svc.config.AdminPasswords {
		hdr := "Basic " + base64.StdEncoding.EncodeToString([]byte("admin:"+pw))
		validAuthHeaders = append(validAuthHeaders, hdr)
	}

	// for paths that this middleware is applied to, enforce that the auth header must exist and match one of the known passwords
	return func(c echo.Context) error {
		hdr := c.Request().Header.Get("Authorization")
		if hdr == "" {
			c.Response().Header().Set("WWW-Authenticate", "Basic")
			return echo.ErrUnauthorized
		}
		for _, val := range validAuthHeaders {
			if subtle.ConstantTimeCompare([]byte(hdr), []byte(val)) == 1 {
				return next(c)
			}
		}
		c.Response().Header().Set("WWW-Authenticate", "Basic")
		return echo.ErrUnauthorized
	}
}
