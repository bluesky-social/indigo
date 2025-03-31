package main

import (
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"slices"
	"strconv"
	"strings"

	"github.com/bluesky-social/indigo/cmd/relayered/relay/slurper"
	"github.com/bluesky-social/indigo/cmd/relayered/relay"

	"github.com/labstack/echo/v4"
	dto "github.com/prometheus/client_model/go"
	"gorm.io/gorm"
)

func (svc *Service) handleAdminSetSubsEnabled(e echo.Context) error {
	enabled, err := strconv.ParseBool(e.QueryParam("enabled"))
	if err != nil {
		return &echo.HTTPError{
			Code:    400,
			Message: err.Error(),
		}
	}

	return svc.relay.Slurper.SetNewSubsDisabled(!enabled)
}

func (svc *Service) handleAdminGetSubsEnabled(e echo.Context) error {
	return e.JSON(200, map[string]bool{
		"enabled": !svc.relay.Slurper.GetNewSubsDisabledState(),
	})
}

func (svc *Service) handleAdminGetNewPDSPerDayRateLimit(e echo.Context) error {
	limit := svc.relay.Slurper.GetNewPDSPerDayLimit()
	return e.JSON(200, map[string]int64{
		"limit": limit,
	})
}

func (svc *Service) handleAdminSetNewPDSPerDayRateLimit(e echo.Context) error {
	limit, err := strconv.ParseInt(e.QueryParam("limit"), 10, 64)
	if err != nil {
		return &echo.HTTPError{
			Code:    400,
			Message: fmt.Errorf("failed to parse limit: %w", err).Error(),
		}
	}

	err = svc.relay.Slurper.SetNewPDSPerDayLimit(limit)
	if err != nil {
		return &echo.HTTPError{
			Code:    500,
			Message: fmt.Errorf("failed to set new PDS per day rate limit: %w", err).Error(),
		}
	}

	return nil
}

func (svc *Service) handleAdminTakeDownRepo(e echo.Context) error {
	ctx := e.Request().Context()

	var body map[string]string
	if err := e.Bind(&body); err != nil {
		return err
	}
	did, ok := body["did"]
	if !ok {
		return &echo.HTTPError{
			Code:    400,
			Message: "must specify did parameter in body",
		}
	}

	err := svc.relay.TakeDownRepo(ctx, did)
	if err != nil {
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
	return nil
}

func (svc *Service) handleAdminReverseTakedown(e echo.Context) error {
	did := e.QueryParam("did")
	ctx := e.Request().Context()
	err := svc.relay.ReverseTakedown(ctx, did)

	if err != nil {
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

	return nil
}

type ListTakedownsResponse struct {
	Dids   []string `json:"dids"`
	Cursor int64    `json:"cursor,omitempty"`
}

func (svc *Service) handleAdminListRepoTakeDowns(e echo.Context) error {
	ctx := e.Request().Context()
	haveMinId := false
	minId := int64(-1)
	qmin := e.QueryParam("cursor")
	if qmin != "" {
		tmin, err := strconv.ParseInt(qmin, 10, 64)
		if err != nil {
			return &echo.HTTPError{Code: 400, Message: "bad cursor"}
		}
		minId = tmin
		haveMinId = true
	}
	limit := 1000
	wat := svc.db.Model(slurper.Account{}).WithContext(ctx).Select("id", "did").Where("taken_down = TRUE")
	if haveMinId {
		wat = wat.Where("id > ?", minId)
	}
	//var users []slurper.Account
	rows, err := wat.Order("id").Limit(limit).Rows()
	if err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, "oops").WithInternal(err)
	}
	var out ListTakedownsResponse
	for rows.Next() {
		var id int64
		var did string
		err := rows.Scan(&id, &did)
		if err != nil {
			return echo.NewHTTPError(http.StatusInternalServerError, "oops").WithInternal(err)
		}
		out.Dids = append(out.Dids, did)
		out.Cursor = id
	}
	if len(out.Dids) < limit {
		out.Cursor = 0
	}
	return e.JSON(200, out)
}

func (svc *Service) handleAdminGetUpstreamConns(e echo.Context) error {
	return e.JSON(200, svc.relay.Slurper.GetActiveList())
}

type rateLimit struct {
	Max           float64 `json:"Max"`
	WindowSeconds float64 `json:"Window"`
}

type enrichedPDS struct {
	slurper.PDS
	HasActiveConnection    bool      `json:"HasActiveConnection"`
	EventsSeenSinceStartup uint64    `json:"EventsSeenSinceStartup"`
	PerSecondEventRate     rateLimit `json:"PerSecondEventRate"`
	PerHourEventRate       rateLimit `json:"PerHourEventRate"`
	PerDayEventRate        rateLimit `json:"PerDayEventRate"`
	UserCount              int64     `json:"UserCount"`
}

type UserCount struct {
	PDSID     uint  `gorm:"column:pds"`
	UserCount int64 `gorm:"column:user_count"`
}

func (svc *Service) handleListPDSs(e echo.Context) error {
	var pds []slurper.PDS
	if err := svc.db.Find(&pds).Error; err != nil {
		return err
	}

	enrichedPDSs := make([]enrichedPDS, len(pds))

	activePDSHosts := svc.relay.Slurper.GetActiveList()

	for i, p := range pds {
		enrichedPDSs[i].PDS = p
		enrichedPDSs[i].HasActiveConnection = false
		for _, host := range activePDSHosts {
			if strings.ToLower(host) == strings.ToLower(p.Host) {
				enrichedPDSs[i].HasActiveConnection = true
				break
			}
		}
		var m = &dto.Metric{}
		if err := relay.EventsReceivedCounter.WithLabelValues(p.Host).Write(m); err != nil {
			enrichedPDSs[i].EventsSeenSinceStartup = 0
			continue
		}
		enrichedPDSs[i].EventsSeenSinceStartup = uint64(m.Counter.GetValue())

		enrichedPDSs[i].PerSecondEventRate = rateLimit{
			Max:           p.RateLimit,
			WindowSeconds: 1,
		}

		enrichedPDSs[i].PerHourEventRate = rateLimit{
			Max:           float64(p.HourlyEventLimit),
			WindowSeconds: 3600,
		}

		enrichedPDSs[i].PerDayEventRate = rateLimit{
			Max:           float64(p.DailyEventLimit),
			WindowSeconds: 86400,
		}
	}

	return e.JSON(200, enrichedPDSs)
}

func (svc *Service) handleAdminListConsumers(e echo.Context) error {

	consumers := svc.relay.ListConsumers()
	return e.JSON(200, consumers)
}

func (svc *Service) handleAdminKillUpstreamConn(e echo.Context) error {
	host := strings.TrimSpace(e.QueryParam("host"))
	if host == "" {
		return &echo.HTTPError{
			Code:    400,
			Message: "must pass a valid host",
		}
	}

	block := strings.ToLower(e.QueryParam("block")) == "true"

	if err := svc.relay.Slurper.KillUpstreamConnection(host, block); err != nil {
		if errors.Is(err, slurper.ErrNoActiveConnection) {
			return &echo.HTTPError{
				Code:    400,
				Message: "no active connection to given host",
			}
		}
		return err
	}

	return e.JSON(200, map[string]any{
		"success": "true",
	})
}

func (svc *Service) handleBlockPDS(e echo.Context) error {
	host := strings.TrimSpace(e.QueryParam("host"))
	if host == "" {
		return &echo.HTTPError{
			Code:    400,
			Message: "must pass a valid host",
		}
	}

	// Set the block flag to true in the DB
	if err := svc.db.Model(&slurper.PDS{}).Where("host = ?", host).Update("blocked", true).Error; err != nil {
		return err
	}

	// don't care if this errors, but we should try to disconnect something we just blocked
	_ = svc.relay.Slurper.KillUpstreamConnection(host, false)

	return e.JSON(200, map[string]any{
		"success": "true",
	})
}

func (svc *Service) handleUnblockPDS(e echo.Context) error {
	host := strings.TrimSpace(e.QueryParam("host"))
	if host == "" {
		return &echo.HTTPError{
			Code:    400,
			Message: "must pass a valid host",
		}
	}

	// Set the block flag to false in the DB
	if err := svc.db.Model(&slurper.PDS{}).Where("host = ?", host).Update("blocked", false).Error; err != nil {
		return err
	}

	return e.JSON(200, map[string]any{
		"success": "true",
	})
}

type bannedDomains struct {
	BannedDomains []string `json:"banned_domains"`
}

func (svc *Service) handleAdminListDomainBans(c echo.Context) error {
	var all []slurper.DomainBan
	if err := svc.db.Find(&all).Error; err != nil {
		return err
	}

	resp := bannedDomains{
		BannedDomains: []string{},
	}
	for _, b := range all {
		resp.BannedDomains = append(resp.BannedDomains, b.Domain)
	}

	return c.JSON(200, resp)
}

type banDomainBody struct {
	Domain string
}

func (svc *Service) handleAdminBanDomain(c echo.Context) error {
	var body banDomainBody
	if err := c.Bind(&body); err != nil {
		return err
	}

	// Check if the domain is already banned
	var existing slurper.DomainBan
	if err := svc.db.Where("domain = ?", body.Domain).First(&existing).Error; err == nil {
		return &echo.HTTPError{
			Code:    400,
			Message: "domain is already banned",
		}
	}

	if err := svc.db.Create(&slurper.DomainBan{
		Domain: body.Domain,
	}).Error; err != nil {
		return err
	}

	return c.JSON(200, map[string]any{
		"success": "true",
	})
}

func (svc *Service) handleAdminUnbanDomain(c echo.Context) error {
	var body banDomainBody
	if err := c.Bind(&body); err != nil {
		return err
	}

	if err := svc.db.Where("domain = ?", body.Domain).Delete(&slurper.DomainBan{}).Error; err != nil {
		return err
	}

	return c.JSON(200, map[string]any{
		"success": "true",
	})
}

type RateLimitChangeRequest struct {
	Host string `json:"host"`
	slurper.PDSRates
}

func (svc *Service) handleAdminChangePDSRateLimits(e echo.Context) error {
	var body RateLimitChangeRequest
	if err := e.Bind(&body); err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, fmt.Sprintf("invalid body: %s", err))
	}

	// Get the PDS from the DB
	var pds slurper.PDS
	if err := svc.db.Where("host = ?", body.Host).First(&pds).Error; err != nil {
		return err
	}

	// Update the rate limits in the DB
	pds.RateLimit = float64(body.PerSecond)
	pds.HourlyEventLimit = body.PerHour
	pds.DailyEventLimit = body.PerDay
	pds.RepoLimit = body.RepoLimit

	if err := svc.db.Save(&pds).Error; err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, fmt.Errorf("failed to save rate limit changes: %w", err))
	}

	// Update the rate limit in the limiter
	limits := svc.relay.Slurper.GetOrCreateLimiters(pds.ID, body.PerSecond, body.PerHour, body.PerDay)
	limits.PerSecond.SetLimit(body.PerSecond)
	limits.PerHour.SetLimit(body.PerHour)
	limits.PerDay.SetLimit(body.PerDay)

	return e.JSON(200, map[string]any{
		"success": "true",
	})
}

func (svc *Service) handleAdminAddTrustedDomain(e echo.Context) error {
	domain := e.QueryParam("domain")
	if domain == "" {
		return fmt.Errorf("must specify domain in query parameter")
	}

	// Check if the domain is already trusted
	trustedDomains := svc.relay.Slurper.GetTrustedDomains()
	if slices.Contains(trustedDomains, domain) {
		return &echo.HTTPError{
			Code:    400,
			Message: "domain is already trusted",
		}
	}

	if err := svc.relay.Slurper.AddTrustedDomain(domain); err != nil {
		return err
	}

	return e.JSON(200, map[string]any{
		"success": true,
	})
}

type AdminRequestCrawlRequest struct {
	Hostname string `json:"hostname"`

	// optional:
	slurper.PDSRates
}

func (svc *Service) handleAdminRequestCrawl(e echo.Context) error {
	ctx := e.Request().Context()

	var body AdminRequestCrawlRequest
	if err := e.Bind(&body); err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, fmt.Sprintf("invalid body: %s", err))
	}

	host := body.Hostname
	if host == "" {
		return echo.NewHTTPError(http.StatusBadRequest, "must pass hostname")
	}

	if !strings.HasPrefix(host, "http://") && !strings.HasPrefix(host, "https://") {
		if svc.ssl {
			host = "https://" + host
		} else {
			host = "http://" + host
		}
	}

	u, err := url.Parse(host)
	if err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, "failed to parse hostname")
	}

	if u.Scheme == "http" && svc.ssl {
		return echo.NewHTTPError(http.StatusBadRequest, "this server requires https")
	}

	if u.Scheme == "https" && !svc.ssl {
		return echo.NewHTTPError(http.StatusBadRequest, "this server does not support https")
	}

	if u.Path != "" {
		return echo.NewHTTPError(http.StatusBadRequest, "must pass hostname without path")
	}

	if u.Query().Encode() != "" {
		return echo.NewHTTPError(http.StatusBadRequest, "must pass hostname without query")
	}

	host = u.Host // potentially hostname:port

	banned, err := svc.relay.DomainIsBanned(ctx, host)
	if banned {
		return echo.NewHTTPError(http.StatusUnauthorized, "domain is banned")
	}

	// Skip checking if the server is online for now
	rateOverrides := body.PDSRates
	rateOverrides.FromSlurper(svc.relay.Slurper)

	return svc.relay.Slurper.SubscribeToPds(ctx, host, true, true, &rateOverrides) // Override Trusted Domain Check
}
