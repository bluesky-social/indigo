package bgs

import (
	"errors"
	"strconv"
	"strings"

	"github.com/labstack/echo/v4"
)

func (bgs *BGS) handleAdminBlockRepoStream(e echo.Context) error {
	panic("TODO")
}

func (bgs *BGS) handleAdminSetSubsEnabled(e echo.Context) error {
	enabled, err := strconv.ParseBool(e.QueryParam("enabled"))
	if err != nil {
		return err
	}

	return bgs.slurper.SetNewSubsDisabled(!enabled)
}

func (bgs *BGS) handleAdminTakeDownRepo(e echo.Context) error {
	did := e.QueryParam("did")
	ctx := e.Request().Context()

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

	if err := bgs.slurper.KillUpstreamConnection(host); err != nil {
		if errors.Is(err, ErrNoActiveConnection) {
			return &echo.HTTPError{
				Code:    400,
				Message: "no active connection to given host",
			}
		}
		return err
	}

	return nil
}
