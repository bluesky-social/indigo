package bgs

import (
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/bluesky-social/indigo/models"
	"github.com/labstack/echo/v4"
	dto "github.com/prometheus/client_model/go"
	"golang.org/x/time/rate"
	"gorm.io/gorm"
)

func (bgs *BGS) handleAdminBlockRepoStream(e echo.Context) error {
	panic("TODO")
}

func (bgs *BGS) handleAdminSetSubsEnabled(e echo.Context) error {
	enabled, err := strconv.ParseBool(e.QueryParam("enabled"))
	if err != nil {
		return &echo.HTTPError{
			Code:    400,
			Message: err.Error(),
		}
	}

	return bgs.slurper.SetNewSubsDisabled(!enabled)
}

func (bgs *BGS) handleAdminGetSubsEnabled(e echo.Context) error {
	return e.JSON(200, map[string]bool{
		"enabled": !bgs.slurper.GetNewSubsDisabledState(),
	})
}

func (bgs *BGS) handleAdminTakeDownRepo(e echo.Context) error {
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

	err := bgs.TakeDownRepo(ctx, did)
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

func (bgs *BGS) handleAdminReverseTakedown(e echo.Context) error {
	did := e.QueryParam("did")
	ctx := e.Request().Context()
	err := bgs.ReverseTakedown(ctx, did)

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

func (bgs *BGS) handleAdminGetUpstreamConns(e echo.Context) error {
	return e.JSON(200, bgs.slurper.GetActiveList())
}

type rateLimit struct {
	MaxEventsPerSecond float64 `json:"MaxEventsPerSecond"`
	TokenCount         float64 `json:"TokenCount"`
}

type enrichedPDS struct {
	models.PDS
	HasActiveConnection    bool      `json:"HasActiveConnection"`
	EventsSeenSinceStartup uint64    `json:"EventsSeenSinceStartup"`
	IngestRate             rateLimit `json:"IngestRate"`
	CrawlRate              rateLimit `json:"CrawlRate"`
}

func (bgs *BGS) handleListPDSs(e echo.Context) error {
	var pds []models.PDS
	if err := bgs.db.Find(&pds).Error; err != nil {
		return err
	}

	enrichedPDSs := make([]enrichedPDS, len(pds))

	activePDSHosts := bgs.slurper.GetActiveList()

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
		if err := eventsReceivedCounter.WithLabelValues(p.Host).Write(m); err != nil {
			enrichedPDSs[i].EventsSeenSinceStartup = 0
			continue
		}
		enrichedPDSs[i].EventsSeenSinceStartup = uint64(m.Counter.GetValue())

		// Get the ingest rate limit for this PDS
		ingestRate := rateLimit{
			MaxEventsPerSecond: p.RateLimit,
		}

		limiter := bgs.slurper.GetLimiter(p.ID)
		if limiter != nil {
			ingestRate.TokenCount = limiter.Tokens()
		}

		enrichedPDSs[i].IngestRate = ingestRate

		// Get the crawl rate limit for this PDS
		crawlRate := rateLimit{
			MaxEventsPerSecond: p.CrawlRateLimit,
		}

		limiter = bgs.Index.GetLimiter(p.ID)
		if limiter != nil {
			crawlRate.TokenCount = limiter.Tokens()
		}

		enrichedPDSs[i].CrawlRate = crawlRate
	}

	return e.JSON(200, enrichedPDSs)
}

type consumer struct {
	ID             uint64    `json:"id"`
	RemoteAddr     string    `json:"remote_addr"`
	UserAgent      string    `json:"user_agent"`
	EventsConsumed uint64    `json:"events_consumed"`
	ConnectedAt    time.Time `json:"connected_at"`
}

func (bgs *BGS) handleAdminListConsumers(e echo.Context) error {
	bgs.consumersLk.RLock()
	defer bgs.consumersLk.RUnlock()

	consumers := make([]consumer, 0, len(bgs.consumers))
	for id, c := range bgs.consumers {
		var m = &dto.Metric{}
		if err := c.EventsSent.Write(m); err != nil {
			continue
		}
		consumers = append(consumers, consumer{
			ID:             id,
			RemoteAddr:     c.RemoteAddr,
			UserAgent:      c.UserAgent,
			EventsConsumed: uint64(m.Counter.GetValue()),
			ConnectedAt:    c.ConnectedAt,
		})
	}

	return e.JSON(200, consumers)
}

func (bgs *BGS) handleAdminKillUpstreamConn(e echo.Context) error {
	host := strings.TrimSpace(e.QueryParam("host"))
	if host == "" {
		return &echo.HTTPError{
			Code:    400,
			Message: "must pass a valid host",
		}
	}

	block := strings.ToLower(e.QueryParam("block")) == "true"

	if err := bgs.slurper.KillUpstreamConnection(host, block); err != nil {
		if errors.Is(err, ErrNoActiveConnection) {
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

func (bgs *BGS) handleBlockPDS(e echo.Context) error {
	host := strings.TrimSpace(e.QueryParam("host"))
	if host == "" {
		return &echo.HTTPError{
			Code:    400,
			Message: "must pass a valid host",
		}
	}

	// Set the block flag to true in the DB
	if err := bgs.db.Model(&models.PDS{}).Where("host = ?", host).Update("blocked", true).Error; err != nil {
		return err
	}

	return e.JSON(200, map[string]any{
		"success": "true",
	})
}

func (bgs *BGS) handleUnblockPDS(e echo.Context) error {
	host := strings.TrimSpace(e.QueryParam("host"))
	if host == "" {
		return &echo.HTTPError{
			Code:    400,
			Message: "must pass a valid host",
		}
	}

	// Set the block flag to false in the DB
	if err := bgs.db.Model(&models.PDS{}).Where("host = ?", host).Update("blocked", false).Error; err != nil {
		return err
	}

	return e.JSON(200, map[string]any{
		"success": "true",
	})
}

type bannedDomains struct {
	BannedDomains []string `json:"banned_domains"`
}

func (bgs *BGS) handleAdminListDomainBans(c echo.Context) error {
	var all []models.DomainBan
	if err := bgs.db.Find(&all).Error; err != nil {
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

func (bgs *BGS) handleAdminBanDomain(c echo.Context) error {
	var body banDomainBody
	if err := c.Bind(&body); err != nil {
		return err
	}

	// Check if the domain is already banned
	var existing models.DomainBan
	if err := bgs.db.Where("domain = ?", body.Domain).First(&existing).Error; err == nil {
		return &echo.HTTPError{
			Code:    400,
			Message: "domain is already banned",
		}
	}

	if err := bgs.db.Create(&models.DomainBan{
		Domain: body.Domain,
	}).Error; err != nil {
		return err
	}

	return c.JSON(200, map[string]any{
		"success": "true",
	})
}

func (bgs *BGS) handleAdminUnbanDomain(c echo.Context) error {
	var body banDomainBody
	if err := c.Bind(&body); err != nil {
		return err
	}

	if err := bgs.db.Where("domain = ?", body.Domain).Delete(&models.DomainBan{}).Error; err != nil {
		return err
	}

	return c.JSON(200, map[string]any{
		"success": "true",
	})
}

func (bgs *BGS) handleAdminChangePDSRateLimit(e echo.Context) error {
	host := strings.TrimSpace(e.QueryParam("host"))
	if host == "" {
		return &echo.HTTPError{
			Code:    400,
			Message: "must pass a valid host",
		}
	}

	// Get the new rate limit
	limit, err := strconv.ParseFloat(e.QueryParam("limit"), 64)
	if err != nil {
		return &echo.HTTPError{
			Code:    400,
			Message: "must pass a valid limit",
		}
	}

	// Get the PDS from the DB
	var pds models.PDS
	if err := bgs.db.Where("host = ?", host).First(&pds).Error; err != nil {
		return err
	}

	// Update the rate limit in the DB
	if err := bgs.db.Model(&pds).Update("rate_limit", limit).Error; err != nil {
		return err
	}

	// Update the rate limit in the limiter
	limiter := bgs.slurper.GetLimiter(pds.ID)
	if limiter == nil {
		limiter = rate.NewLimiter(rate.Limit(limit), 1)
	}
	limiter.SetLimit(rate.Limit(limit))

	return e.JSON(200, map[string]any{
		"success": "true",
	})
}

func (bgs *BGS) handleAdminChangePDSCrawlLimit(e echo.Context) error {
	host := strings.TrimSpace(e.QueryParam("host"))
	if host == "" {
		return &echo.HTTPError{
			Code:    400,
			Message: "must pass a valid host",
		}
	}

	// Get the new crawl limit
	limit, err := strconv.ParseFloat(e.QueryParam("limit"), 64)
	if err != nil {
		return &echo.HTTPError{
			Code:    400,
			Message: "must pass a valid limit",
		}
	}

	// Get the PDS from the DB
	var pds models.PDS
	if err := bgs.db.Where("host = ?", host).First(&pds).Error; err != nil {
		return err
	}

	// Update the crawl limit in the DB
	if err := bgs.db.Model(&pds).Update("crawl_rate_limit", limit).Error; err != nil {
		return err
	}

	// Update the crawl limit in the limiter
	limiter := bgs.Index.GetLimiter(pds.ID)
	if limiter != nil {
		limiter = rate.NewLimiter(rate.Limit(limit), 1)
	}
	limiter.SetLimit(rate.Limit(limit))

	return e.JSON(200, map[string]any{
		"success": "true",
	})
}
