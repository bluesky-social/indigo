package main

import (
	"errors"
	"fmt"
	"gorm.io/gorm"
	"net/http"
	"strconv"

	comatprototypes "github.com/bluesky-social/indigo/api/atproto"
	"github.com/bluesky-social/indigo/atproto/syntax"
	"github.com/labstack/echo/v4"
	"go.opentelemetry.io/otel"
)

type XRPCError struct {
	Message string `json:"message"`
}

func (s *Service) RegisterHandlersAppBsky(e *echo.Echo) error {
	return nil
}

func (s *Service) RegisterHandlersComAtproto(e *echo.Echo) error {
	e.GET("/xrpc/com.atproto.sync.getLatestCommit", s.HandleComAtprotoSyncGetLatestCommit)
	e.GET("/xrpc/com.atproto.sync.listRepos", s.HandleComAtprotoSyncListRepos)
	e.POST("/xrpc/com.atproto.sync.requestCrawl", s.HandleComAtprotoSyncRequestCrawl)
	return nil
}

func (s *Service) HandleComAtprotoSyncSubscribeRepos(c echo.Context) error {
	var since *int64
	if sinceVal := c.QueryParam("cursor"); sinceVal != "" {
		sval, err := strconv.ParseInt(sinceVal, 10, 64)
		if err != nil {
			return err
		}
		since = &sval
	}
	return s.relay.HandleSubscribeRepos(c.Response(), c.Request(), since, c.RealIP())
}

func (s *Service) HandleComAtprotoSyncGetLatestCommit(c echo.Context) error {
	ctx, span := otel.Tracer("server").Start(c.Request().Context(), "HandleComAtprotoSyncGetLatestCommit")
	defer span.End()
	did := c.QueryParam("did")

	_, err := syntax.ParseDID(did)
	if err != nil {
		return c.JSON(http.StatusBadRequest, XRPCError{Message: fmt.Sprintf("invalid did: %s", did)})
	}

	var out *comatprototypes.SyncGetLatestCommit_Output
	var handleErr error
	// func (s *Service) handleComAtprotoSyncGetLatestCommit(ctx context.Context,did string) (*comatprototypes.SyncGetLatestCommit_Output, error)
	out, handleErr = s.handleComAtprotoSyncGetLatestCommit(ctx, did)
	if handleErr != nil {
		return handleErr
	}
	return c.JSON(200, out)
}

func (s *Service) HandleComAtprotoSyncListRepos(c echo.Context) error {
	ctx, span := otel.Tracer("server").Start(c.Request().Context(), "HandleComAtprotoSyncListRepos")
	defer span.End()

	cursorQuery := c.QueryParam("cursor")
	limitQuery := c.QueryParam("limit")

	var err error

	limit := 500
	if limitQuery != "" {
		limit, err = strconv.Atoi(limitQuery)
		if err != nil || limit < 1 || limit > 1000 {
			return c.JSON(http.StatusBadRequest, XRPCError{Message: fmt.Sprintf("invalid limit: %s", limitQuery)})
		}
	}

	cursor := int64(0)
	if cursorQuery != "" {
		cursor, err = strconv.ParseInt(cursorQuery, 10, 64)
		if err != nil || cursor < 0 {
			return c.JSON(http.StatusBadRequest, XRPCError{Message: fmt.Sprintf("invalid cursor: %s", cursorQuery)})
		}
	}

	out, handleErr := s.handleComAtprotoSyncListRepos(ctx, cursor, limit)
	if handleErr != nil {
		return handleErr
	}
	return c.JSON(200, out)
}

// HandleComAtprotoSyncGetRepo handles /xrpc/com.atproto.sync.getRepo
// returns 3xx to same URL at source PDS
func (s *Service) HandleComAtprotoSyncGetRepo(c echo.Context) error {
	ctx, span := otel.Tracer("server").Start(c.Request().Context(), "HandleComAtprotoSyncGetRepo")
	defer span.End()
	// XXX: this is not how to fetch query params...
	// no request object, only params
	params := c.QueryParams()
	var did syntax.DID
	hasDid := false
	for paramName, pvl := range params {
		switch paramName {
		case "did":
			if len(pvl) == 1 {
				d, err := syntax.ParseDID(pvl[0])
				if err != nil {
					return err // XXX: better error
				}
				did = d
				hasDid = true
			} else if len(pvl) > 1 {
				return c.JSON(http.StatusBadRequest, XRPCError{Message: "only allow one did param"})
			}
		case "since":
			// ok
		default:
			return c.JSON(http.StatusBadRequest, XRPCError{Message: fmt.Sprintf("invalid param: %s", paramName)})
		}
	}
	if !hasDid {
		return c.JSON(http.StatusBadRequest, XRPCError{Message: "need did param"})
	}

	acc, err := s.relay.GetAccount(ctx, did)
	if err != nil {
		// TODO: better error
		return err
	}

	host, err := s.relay.GetHost(ctx, acc.HostID)
	if err != nil {
		// TODO: better error
		return err
	}

	// TODO: proper error responses
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return c.JSON(http.StatusNotFound, XRPCError{Message: "NULL"})
		}
		s.logger.Error("user.pds.host lookup", "err", err)
		return c.JSON(http.StatusInternalServerError, XRPCError{Message: "sorry"})
	}

	nextUrl := *(c.Request().URL)
	nextUrl.Host = host.Hostname
	if nextUrl.Scheme == "" {
		nextUrl.Scheme = "https"
	}
	return c.Redirect(http.StatusFound, nextUrl.String())
}

func (s *Service) HandleComAtprotoSyncRequestCrawl(c echo.Context) error {
	ctx, span := otel.Tracer("server").Start(c.Request().Context(), "HandleComAtprotoSyncRequestCrawl")
	defer span.End()

	var body comatprototypes.SyncRequestCrawl_Input
	if err := c.Bind(&body); err != nil {
		return c.JSON(http.StatusBadRequest, XRPCError{Message: fmt.Sprintf("invalid body: %s", err)})
	}
	var handleErr error
	// func (s *Service) handleComAtprotoSyncRequestCrawl(ctx context.Context,body *comatprototypes.SyncRequestCrawl_Input) error
	handleErr = s.handleComAtprotoSyncRequestCrawl(ctx, &body)
	if handleErr != nil {
		return handleErr
	}
	return nil
}
