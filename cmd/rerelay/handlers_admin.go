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
	"github.com/bluesky-social/indigo/cmd/rerelay/relay"
	"github.com/bluesky-social/indigo/cmd/rerelay/relay/models"

	"github.com/labstack/echo/v4"
	dto "github.com/prometheus/client_model/go"
	"gorm.io/gorm"
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
	s.relay.Config.DisableNewHosts = !enabled
	return c.JSON(http.StatusOK, map[string]any{
		"success": "true",
	})
}

func (s *Service) handleAdminGetSubsEnabled(c echo.Context) error {
	return c.JSON(http.StatusOK, map[string]bool{
		"enabled": s.relay.Config.DisableNewHosts,
	})
}

func (s *Service) handleAdminGetNewHostPerDayRateLimit(c echo.Context) error {
	return c.JSON(http.StatusOK, map[string]int64{
		"limit": s.relay.Slurper.NewHostPerDayLimiter.Limit(),
	})
}

func (s *Service) handleAdminSetNewHostPerDayRateLimit(c echo.Context) error {
	limit, err := strconv.ParseInt(c.QueryParam("limit"), 10, 64)
	if err != nil {
		return &echo.HTTPError{Code: http.StatusBadRequest, Message: fmt.Errorf("failed to parse limit: %w", err).Error()}
	}

	s.relay.Slurper.NewHostPerDayLimiter.SetLimit(limit)
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

	if err := s.relay.UpdateAccountStatus(ctx, did, models.AccountStatusTakendown); err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
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
	return c.JSON(http.StatusOK, map[string]any{
		"success": "true",
	})
}

func (s *Service) handleAdminReverseTakedown(c echo.Context) error {
	ctx := c.Request().Context()

	did, err := syntax.ParseDID(c.QueryParam("did"))
	if err != nil {
		return err
	}

	if err := s.relay.UpdateAccountStatus(ctx, did, models.AccountStatusActive); err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
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
	ID               uint64
	CreatedAt        time.Time
	Host             string
	SSL              bool
	Cursor           int64
	Registered       bool
	Blocked          bool
	RateLimit        float64
	CrawlRateLimit   float64
	RepoCount        int64
	RepoLimit        int64
	HourlyEventLimit int64
	DailyEventLimit  int64

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
			//TODO: RateLimit
			//TODO: CrawlRateLimit
			RepoCount: host.AccountCount,
			RepoLimit: host.AccountLimit,
			//HourlyEventLimit
			//DailyEventLimit

			HasActiveConnection: isActive,
			UserCount:           host.AccountCount,
		}

		// pull event counter metrics from prometheus
		var m = &dto.Metric{}
		if err := relay.EventsReceivedCounter.WithLabelValues(host.Hostname).Write(m); err != nil {
			hostInfos[i].EventsSeenSinceStartup = 0
			continue
		}
		hostInfos[i].EventsSeenSinceStartup = uint64(m.Counter.GetValue())

		/* XXX: compute these from account limit
		hostInfos[i].PerSecondEventRate = rateLimit{
			Max:           p.RateLimit,
			WindowSeconds: 1,
		}
		hostInfos[i].PerHourEventRate = rateLimit{
			Max:           float64(p.HourlyEventLimit),
			WindowSeconds: 3600,
		}
		hostInfos[i].PerDayEventRate = rateLimit{
			Max:           float64(p.DailyEventLimit),
			WindowSeconds: 86400,
		}
		*/
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

	return c.JSON(http.StatusOK, map[string]any{
		"success": "true",
	})
}

type RateLimitChangeRequest struct {
	Host string `json:"host"`
	relay.HostRates
}

/* XXX: finish rate limit stuff
func (s *Service) handleAdminChangeHostRateLimits(c echo.Context) error {
	var body RateLimitChangeRequest
	if err := c.Bind(&body); err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, fmt.Sprintf("invalid body: %s", err))
	}

	var pds models.Host
	if err := s.db.Where("host = ?", body.Host).First(&pds).Error; err != nil {
		return err
	}

	// Update the rate limits in the DB
	pds.RateLimit = float64(body.PerSecond)
	pds.HourlyEventLimit = body.PerHour
	pds.DailyEventLimit = body.PerDay
	pds.RepoLimit = body.RepoLimit

	if err := s.db.Save(&pds).Error; err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, fmt.Errorf("failed to save rate limit changes: %w", err))
	}

	// Update the rate limit in the limiter
	limits := s.relay.Slurper.GetOrCreateLimiters(pds.ID, body.PerSecond, body.PerHour, body.PerDay)
	limits.PerSecond.SetLimit(body.PerSecond)
	limits.PerHour.SetLimit(body.PerHour)
	limits.PerDay.SetLimit(body.PerDay)

	return c.JSON(http.StatusOK, map[string]any{
		"success": "true",
	})
}
*/

/* XXX: DREPRECATED
func (s *Service) handleAdminAddTrustedDomain(c echo.Context) error {
	domain := c.QueryParam("domain")
	if domain == "" {
		return fmt.Errorf("must specify domain in query parameter")
	}

	// Check if the domain is already trusted
	trustedDomains := s.relay.Slurper.GetTrustedDomains()
	if slices.Contains(trustedDomains, domain) {
		return &echo.HTTPError{
			Code:    http.StatusBadRequest,
			Message: "domain is already trusted",
		}
	}

	if err := s.relay.Slurper.AddTrustedDomain(domain); err != nil {
		return err
	}

	return c.JSON(http.StatusOK, map[string]any{
		"success": true,
	})
}
*/
