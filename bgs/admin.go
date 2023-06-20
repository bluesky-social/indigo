package bgs

import (
	"errors"
	"strconv"
	"strings"

	"github.com/bluesky-social/indigo/models"
	"github.com/labstack/echo/v4"
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

	return bgs.TakeDownRepo(ctx, did)
}

func (bgs *BGS) handleAdminReverseTakedown(e echo.Context) error {
	did := e.QueryParam("did")
	ctx := e.Request().Context()

	return bgs.ReverseTakedown(ctx, did)
}

func (bgs *BGS) handleAdminGetUpstreamConns(e echo.Context) error {
	return e.JSON(200, bgs.slurper.GetActiveList())
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

func (bgs *BGS) handleAdminListDomainBans(c echo.Context) error {
	var all []models.DomainBan
	if err := bgs.db.Find(&all).Error; err != nil {
		return err
	}

	var out []string
	for _, b := range all {
		out = append(out, b.Domain)
	}

	return c.JSON(200, out)
}

type banDomainBody struct {
	Domain string
}

func (bgs *BGS) handleAdminBanDomain(c echo.Context) error {
	var body banDomainBody
	if err := c.Bind(&body); err != nil {
		return err
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
