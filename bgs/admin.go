package bgs

import (
	"strconv"

	"github.com/bluesky-social/indigo/util"
	"github.com/labstack/echo/v4"
)

func (bgs *BGS) handleAdminDeleteRecord(e echo.Context) error {
	puri, err := util.ParseAtUri(e.QueryParam("uri"))
	if err != nil {
		return err
	}

	_ = puri

	panic("TODO")
}

func (bgs *BGS) handleAdminBlockRepoStream(e echo.Context) error {
	panic("TODO")
}

func (bgs *BGS) handleAdminDisableNewSlurps(e echo.Context) error {
	enabled, err := strconv.ParseBool(e.QueryParam("enabled"))
	if err != nil {
		return err
	}

	return bgs.slurper.SetNewSubsDisabled(!enabled)
}

func (bgs *BGS) handleAdminTakedownRepo(e echo.Context) error {
	panic("TODO")
}
