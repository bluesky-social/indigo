package bgs

import (
	"strconv"

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
