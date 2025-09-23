package archiver

import (
	"errors"
	"fmt"
	"net/http"

	"github.com/bluesky-social/indigo/atproto/syntax"
	"github.com/bluesky-social/indigo/bgs"
	"github.com/bluesky-social/indigo/events"
	"github.com/labstack/echo/v4"
	"github.com/labstack/gommon/log"
	"go.opentelemetry.io/otel"
	"gorm.io/gorm"
)

func (s *Archiver) HandleComAtprotoSyncGetRepo(c echo.Context) error {
	ctx, span := otel.Tracer("server").Start(c.Request().Context(), "HandleComAtprotoSyncGetRepo")
	defer span.End()
	did := c.QueryParam("did")
	since := c.QueryParam("since")

	_, err := syntax.ParseDID(did)
	if err != nil {
		return c.JSON(http.StatusBadRequest, bgs.XRPCError{Message: fmt.Sprintf("invalid did: %s", did)})
	}

	c.Response().Header().Set(echo.HeaderContentType, "application/vnd.ipld.car")

	u, err := s.lookupUserByDid(ctx, did)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return echo.NewHTTPError(http.StatusNotFound, "user not found")
		}
		log.Error("failed to lookup user", "err", err, "did", did)
		return echo.NewHTTPError(http.StatusInternalServerError, "failed to lookup user")
	}

	if u.GetTombstoned() {
		return fmt.Errorf("account was deleted")
	}

	if u.GetTakenDown() {
		return fmt.Errorf("account was taken down by the Relay")
	}

	ustatus := u.GetUpstreamStatus()
	if ustatus == events.AccountStatusTakendown {
		return fmt.Errorf("account was taken down by its PDS")
	}

	if ustatus == events.AccountStatusDeactivated {
		return fmt.Errorf("account is temporarily deactivated")
	}

	if ustatus == events.AccountStatusSuspended {
		return fmt.Errorf("account is suspended by its PDS")
	}

	if err := s.repoman.ReadRepo(ctx, u.ID, since, c.Response()); err != nil {
		log.Error("failed to stream repo", "err", err, "did", did)
		return echo.NewHTTPError(http.StatusInternalServerError, "failed to stream repo")
	}

	return nil
}

type HealthStatus struct {
	Status  string `json:"status"`
	Message string `json:"msg,omitempty"`
}

func (s *Archiver) HandleHealthCheck(c echo.Context) error {
	if err := s.db.Exec("SELECT 1").Error; err != nil {
		s.log.Error("healthcheck can't connect to database", "err", err)
		return c.JSON(500, HealthStatus{Status: "error", Message: "can't connect to database"})
	} else {
		return c.JSON(200, HealthStatus{Status: "ok"})
	}
}

var homeMessage string = `
[ insert fancy archiver art here ]
`

func (s *Archiver) HandleHomeMessage(c echo.Context) error {
	return c.String(http.StatusOK, homeMessage)
}
