package main

import (
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	comatproto "github.com/bluesky-social/indigo/api/atproto"
	"github.com/bluesky-social/indigo/atproto/syntax"
	"github.com/bluesky-social/indigo/cmd/relay/relay"
	"github.com/bluesky-social/indigo/cmd/relay/relay/models"

	"github.com/labstack/echo/v4"
	dto "github.com/prometheus/client_model/go"
)

// this is the same as the regular com.atproto.sync.requestCrawl endpoint, except it sets a flag to bypass configuration checks
func (s *Service) handleAdminRequestCrawl(c echo.Context) error {
	var body comatproto.SyncRequestCrawl_Input
	if err := c.Bind(&body); err != nil {
		return &echo.HTTPError{Code: http.StatusBadRequest, Message: fmt.Sprintf("invalid body: %s", err)}
	}

	// func (s *Service) handleComAtprotoSyncRequestCrawl(ctx context.Context,body *comatproto.SyncRequestCrawl_Input) error
	return s.handleComAtprotoSyncRequestCrawl(c, &body, true)
}

func (s *Service) handleAdminSetSubsEnabled(c echo.Context) error {
	enabled, err := strconv.ParseBool(c.QueryParam("enabled"))
	if err != nil {
		return &echo.HTTPError{Code: http.StatusBadRequest, Message: err.Error()}
	}
	s.config.DisableRequestCrawl = !enabled
	return c.JSON(http.StatusOK, map[string]any{
		"success": "true",
	})
}

func (s *Service) handleAdminGetSubsEnabled(c echo.Context) error {
	return c.JSON(http.StatusOK, map[string]bool{
		"enabled": !s.config.DisableRequestCrawl,
	})
}

func (s *Service) handleAdminGetNewHostPerDayRateLimit(c echo.Context) error {
	return c.JSON(http.StatusOK, map[string]int64{
		"limit": s.relay.HostPerDayLimiter.Limit(),
	})
}

func (s *Service) handleAdminSetNewHostPerDayRateLimit(c echo.Context) error {
	limit, err := strconv.ParseInt(c.QueryParam("limit"), 10, 64)
	if err != nil {
		return &echo.HTTPError{Code: http.StatusBadRequest, Message: fmt.Errorf("failed to parse limit: %w", err).Error()}
	}

	s.relay.HostPerDayLimiter.SetLimit(limit)

	// TODO: forward to SiblingRelayHosts
	return c.JSON(http.StatusOK, map[string]any{
		"success": "true",
	})
}

func (s *Service) handleAdminTakeDownRepo(c echo.Context) error {
	ctx := c.Request().Context()

	var body map[string]string
	if err := c.Bind(&body); err != nil {
		return err
	}
	didField, ok := body["did"]
	if !ok {
		return &echo.HTTPError{
			Code:    http.StatusBadRequest,
			Message: "must specify DID parameter in body",
		}
	}
	did, err := syntax.ParseDID(didField)
	if err != nil {
		return err
	}

	if err := s.relay.UpdateAccountLocalStatus(ctx, did, models.AccountStatusTakendown, true); err != nil {
		if errors.Is(err, relay.ErrAccountNotFound) {
			return &echo.HTTPError{
				Code:    http.StatusNotFound,
				Message: "account not found",
			}
		}
		return &echo.HTTPError{
			Code:    http.StatusInternalServerError,
			Message: err.Error(),
		}
	}

	// TODO: forward to SiblingRelayHosts
	return c.JSON(http.StatusOK, map[string]any{
		"success": "true",
	})
}

func (s *Service) handleAdminReverseTakedown(c echo.Context) error {
	ctx := c.Request().Context()

	var body map[string]string
	if err := c.Bind(&body); err != nil {
		return err
	}
	didField, ok := body["did"]
	if !ok {
		return &echo.HTTPError{
			Code:    http.StatusBadRequest,
			Message: "must specify DID parameter in body",
		}
	}
	did, err := syntax.ParseDID(didField)
	if err != nil {
		return err
	}

	if err := s.relay.UpdateAccountLocalStatus(ctx, did, models.AccountStatusActive, true); err != nil {
		if errors.Is(err, relay.ErrAccountNotFound) {
			return &echo.HTTPError{
				Code:    http.StatusNotFound,
				Message: "repo not found",
			}
		}
		return &echo.HTTPError{
			Code:    http.StatusInternalServerError,
			Message: err.Error(),
		}
	}

	// TODO: forward to SiblingRelayHosts
	return c.JSON(http.StatusOK, map[string]any{
		"success": "true",
	})
}

type ListTakedownsResponse struct {
	DIDs   []string `json:"dids"`
	Cursor int64    `json:"cursor,omitempty"`
}

func (s *Service) handleAdminListRepoTakeDowns(c echo.Context) error {
	ctx := c.Request().Context()
	var err error

	limit := 500
	cursor := int64(0)
	cursorQuery := c.QueryParam("cursor")
	if cursorQuery != "" {
		cursor, err = strconv.ParseInt(cursorQuery, 10, 64)
		if err != nil {
			return &echo.HTTPError{Code: http.StatusBadRequest, Message: "invalid cursor param"}
		}
	}

	accounts, err := s.relay.ListAccountTakedowns(ctx, cursor, limit)
	if err != nil {
		return &echo.HTTPError{Code: http.StatusInternalServerError, Message: "failed to list takedowns"}
	}

	out := ListTakedownsResponse{
		DIDs: make([]string, len(accounts)),
	}
	for i, acc := range accounts {
		out.DIDs[i] = acc.DID
		out.Cursor = int64(acc.UID)
	}
	if len(out.DIDs) < limit {
		out.Cursor = 0
	}
	return c.JSON(http.StatusOK, out)
}

func (s *Service) handleAdminGetUpstreamConns(c echo.Context) error {
	return c.JSON(http.StatusOK, s.relay.Slurper.GetActiveSubHostnames())
}

type rateLimit struct {
	Max           float64 `json:"Max"`
	WindowSeconds float64 `json:"Window"`
}

type hostInfo struct {
	// fields from old models.PDS
	ID             uint64
	CreatedAt      time.Time
	Host           string
	SSL            bool
	Cursor         int64
	Registered     bool
	Blocked        bool
	CrawlRateLimit float64
	RepoCount      int64
	RepoLimit      int64

	HasActiveConnection    bool      `json:"HasActiveConnection"`
	EventsSeenSinceStartup uint64    `json:"EventsSeenSinceStartup"`
	PerSecondEventRate     rateLimit `json:"PerSecondEventRate"`
	PerHourEventRate       rateLimit `json:"PerHourEventRate"`
	PerDayEventRate        rateLimit `json:"PerDayEventRate"`
	UserCount              int64     `json:"UserCount"`
}

func (s *Service) handleListHosts(c echo.Context) error {
	ctx := c.Request().Context()

	limit := 10_000
	hosts, err := s.relay.ListHosts(ctx, 0, limit)
	if err != nil {
		return err
	}

	activeHostnames := s.relay.Slurper.GetActiveSubHostnames()
	activeHosts := make(map[string]bool, len(activeHostnames))
	for _, hostname := range activeHostnames {
		activeHosts[hostname] = true
	}

	hostInfos := make([]hostInfo, len(hosts))
	for i, host := range hosts {
		_, isActive := activeHosts[host.Hostname]
		hostInfos[i] = hostInfo{
			ID:         host.ID,
			CreatedAt:  host.CreatedAt,
			Host:       host.Hostname,
			SSL:        !host.NoSSL,
			Cursor:     host.LastSeq,
			Registered: host.Status == models.HostStatusActive, // is this right?
			Blocked:    host.Status == models.HostStatusBanned,
			RepoCount:  host.AccountCount,
			RepoLimit:  host.AccountLimit,

			HasActiveConnection: isActive,
			UserCount:           host.AccountCount,
		}

		// fetch current rate limits
		hostInfos[i].PerSecondEventRate = rateLimit{Max: -1.0, WindowSeconds: 1}
		hostInfos[i].PerHourEventRate = rateLimit{Max: -1.0, WindowSeconds: 3600}
		hostInfos[i].PerDayEventRate = rateLimit{Max: -1.0, WindowSeconds: 86400}
		if isActive {
			slc, err := s.relay.Slurper.GetLimits(host.Hostname)
			if err != nil {
				s.logger.Error("fetching subscribed host limits", "err", err)
			} else {
				hostInfos[i].PerSecondEventRate = rateLimit{
					Max:           float64(slc.PerSecond),
					WindowSeconds: 1,
				}
				hostInfos[i].PerHourEventRate = rateLimit{
					Max:           float64(slc.PerHour),
					WindowSeconds: 3600,
				}
				hostInfos[i].PerDayEventRate = rateLimit{
					Max:           float64(slc.PerDay),
					WindowSeconds: 86400,
				}
			}
		}

		// pull event counter metrics from prometheus
		var m = &dto.Metric{}
		if err := relay.EventsReceivedCounter.WithLabelValues(host.Hostname).Write(m); err != nil {
			hostInfos[i].EventsSeenSinceStartup = 0
			continue
		}
		hostInfos[i].EventsSeenSinceStartup = uint64(m.Counter.GetValue())
	}

	return c.JSON(http.StatusOK, hostInfos)
}

func (s *Service) handleAdminListConsumers(c echo.Context) error {
	return c.JSON(http.StatusOK, s.relay.ListConsumers())
}

func (s *Service) handleAdminKillUpstreamConn(c echo.Context) error {
	queryHost := strings.TrimSpace(c.QueryParam("host"))
	hostname, _, err := relay.ParseHostname(queryHost)
	if err != nil {
		return &echo.HTTPError{
			Code:    http.StatusBadRequest,
			Message: "must pass a valid host",
		}
	}

	banHost := strings.ToLower(c.QueryParam("block")) == "true"

	// TODO: move this method to relay (for updating the database)
	if err := s.relay.Slurper.KillUpstreamConnection(hostname, banHost); err != nil {
		if errors.Is(err, relay.ErrNoActiveConnection) {
			return &echo.HTTPError{
				Code:    http.StatusBadRequest,
				Message: "no active connection to given host",
			}
		}
		return err
	}

	return c.JSON(http.StatusOK, map[string]any{
		"success": "true",
	})
}

func (s *Service) handleBlockHost(c echo.Context) error {
	ctx := c.Request().Context()

	queryHost := strings.TrimSpace(c.QueryParam("host"))
	hostname, _, err := relay.ParseHostname(queryHost)
	if err != nil {
		return &echo.HTTPError{
			Code:    http.StatusBadRequest,
			Message: "must pass a valid hostname",
		}
	}

	host, err := s.relay.GetHost(ctx, hostname)
	if err != nil {
		return err
	}

	if host.Status != models.HostStatusBanned {
		if err := s.relay.UpdateHostStatus(ctx, host.ID, models.HostStatusBanned); err != nil {
			return err
		}
	}

	// kill any active connection (there may not be one, so ignore error)
	_ = s.relay.Slurper.KillUpstreamConnection(host.Hostname, false)

	// TODO: forward to SiblingRelayHosts
	return c.JSON(http.StatusOK, map[string]any{
		"success": "true",
	})
}

func (s *Service) handleUnblockHost(c echo.Context) error {
	ctx := c.Request().Context()

	queryHost := strings.TrimSpace(c.QueryParam("host"))
	hostname, _, err := relay.ParseHostname(queryHost)
	if err != nil {
		return &echo.HTTPError{
			Code:    http.StatusBadRequest,
			Message: "must pass a valid hostname",
		}
	}

	host, err := s.relay.GetHost(ctx, hostname)
	if err != nil {
		return err
	}

	if host.Status != models.HostStatusActive {
		if err := s.relay.UpdateHostStatus(ctx, host.ID, models.HostStatusActive); err != nil {
			return err
		}
	}

	// TODO: forward to SiblingRelayHosts
	return c.JSON(http.StatusOK, map[string]any{
		"success": "true",
	})
}

type bannedDomains struct {
	BannedDomains []string `json:"banned_domains"`
}

func (s *Service) handleAdminListDomainBans(c echo.Context) error {
	ctx := c.Request().Context()

	bans, err := s.relay.ListDomainBans(ctx)
	if err != nil {
		return err
	}

	resp := bannedDomains{
		BannedDomains: make([]string, len(bans)),
	}

	for i, ban := range bans {
		resp.BannedDomains[i] = ban.Domain
	}

	return c.JSON(http.StatusOK, resp)
}

type banDomainBody struct {
	Domain string
}

func (s *Service) handleAdminBanDomain(c echo.Context) error {
	ctx := c.Request().Context()

	var body banDomainBody
	if err := c.Bind(&body); err != nil {
		return err
	}

	err := s.relay.CreateDomainBan(ctx, body.Domain)
	if err != nil {
		return err
	}

	// TODO: forward to SiblingRelayHosts
	return c.JSON(http.StatusOK, map[string]any{
		"success": "true",
	})
}

func (s *Service) handleAdminUnbanDomain(c echo.Context) error {
	ctx := c.Request().Context()

	var body banDomainBody
	if err := c.Bind(&body); err != nil {
		return err
	}

	err := s.relay.RemoveDomainBan(ctx, body.Domain)
	if err != nil {
		return err
	}

	// TODO: forward to SiblingRelayHosts
	return c.JSON(http.StatusOK, map[string]any{
		"success": "true",
	})
}

type RateLimitChangeRequest struct {
	Hostname  string `json:"host"`
	RepoLimit int64  `json:"repo_limit,omitempty"`
}

func (s *Service) handleAdminChangeHostRateLimits(c echo.Context) error {
	ctx := c.Request().Context()

	var body RateLimitChangeRequest
	if err := c.Bind(&body); err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, fmt.Sprintf("invalid body: %s", err))
	}

	hostname, _, err := relay.ParseHostname(body.Hostname)
	if err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, fmt.Sprintf("invalid hostname: %s", err))
	}

	host, err := s.relay.GetHost(ctx, hostname)
	if err != nil {
		// TODO: technically, there could be a database error here or something
		return echo.NewHTTPError(http.StatusNotFound, fmt.Sprintf("unknown hostname: %s", err))
	}

	if err := s.relay.UpdateHostAccountLimit(ctx, host.ID, body.RepoLimit); err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, fmt.Sprintf("failed to update limits: %s", err))
	}

	return c.JSON(http.StatusOK, map[string]any{
		"success": "true",
	})
}
