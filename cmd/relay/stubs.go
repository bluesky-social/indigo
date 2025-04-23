package main

import (
	"fmt"
	"net/http"
	"strconv"

	comatproto "github.com/bluesky-social/indigo/api/atproto"
	"github.com/bluesky-social/indigo/atproto/syntax"
	"github.com/bluesky-social/indigo/cmd/relay/relay"
	"github.com/bluesky-social/indigo/xrpc"

	"github.com/labstack/echo/v4"
	"go.opentelemetry.io/otel"
)

func (s *Service) HandleComAtprotoSyncSubscribeRepos(c echo.Context) error {

	cursorQuery := c.QueryParam("cursor")

	var cursor *int64
	if cursorQuery != "" {
		cval, err := strconv.ParseInt(cursorQuery, 10, 64)
		if err != nil || cval < 0 {
			return c.JSON(http.StatusBadRequest, xrpc.XRPCError{ErrStr: "BadRequest", Message: fmt.Sprintf("cursor parameter invalid: %s", cursorQuery)})
		}
		cursor = &cval
	}

	// pass off HTTP connection to the WebSocket handler
	return s.relay.HandleSubscribeRepos(c.Response(), c.Request(), cursor, c.RealIP())
}

func (s *Service) HandleComAtprotoSyncRequestCrawl(c echo.Context) error {
	_, span := otel.Tracer("server").Start(c.Request().Context(), "HandleComAtprotoSyncRequestCrawl")
	defer span.End()

	var body comatproto.SyncRequestCrawl_Input
	if err := c.Bind(&body); err != nil {
		return c.JSON(http.StatusBadRequest, xrpc.XRPCError{ErrStr: "BadRequest", Message: fmt.Sprintf("invalid body: %s", err)})
	}

	// func (s *Service) handleComAtprotoSyncRequestCrawl(ctx context.Context,body *comatproto.SyncRequestCrawl_Input) error
	return s.handleComAtprotoSyncRequestCrawl(c, &body, false)
}

func (s *Service) HandleComAtprotoSyncListHosts(c echo.Context) error {
	_, span := otel.Tracer("server").Start(c.Request().Context(), "HandleComAtprotoSyncListHosts")
	defer span.End()

	cursorQuery := c.QueryParam("cursor")
	limitQuery := c.QueryParam("limit")

	var err error

	// TODO: verify limits against lexicon
	limit := 200
	if limitQuery != "" {
		limit, err = strconv.Atoi(limitQuery)
		if err != nil || limit < 1 || limit > 1000 {
			return c.JSON(http.StatusBadRequest, xrpc.XRPCError{ErrStr: "BadRequest", Message: fmt.Sprintf("limit parameter invalid or out of range: %s", limitQuery)})
		}
	}

	cursor := int64(0)
	if cursorQuery != "" {
		cursor, err = strconv.ParseInt(cursorQuery, 10, 64)
		if err != nil || cursor < 0 {
			return c.JSON(http.StatusBadRequest, xrpc.XRPCError{ErrStr: "BadRequest", Message: fmt.Sprintf("cursor parameter invalid: %s", cursorQuery)})
		}
	}

	out, handleErr := s.handleComAtprotoSyncListHosts(c, cursor, limit)
	if handleErr != nil || out == nil {
		return handleErr
	}
	return c.JSON(200, out)
}

func (s *Service) HandleComAtprotoSyncGetHostStatus(c echo.Context) error {
	_, span := otel.Tracer("server").Start(c.Request().Context(), "HandleComAtprotoSyncGetHostStatus")
	defer span.End()

	hostnameQuery := c.QueryParam("hostname")

	out, handleErr := s.handleComAtprotoSyncGetHostStatus(c, hostnameQuery)
	if handleErr != nil || out == nil {
		return handleErr
	}
	return c.JSON(200, out)
}

func (s *Service) HandleComAtprotoSyncListRepos(c echo.Context) error {
	_, span := otel.Tracer("server").Start(c.Request().Context(), "HandleComAtprotoSyncListRepos")
	defer span.End()

	cursorQuery := c.QueryParam("cursor")
	limitQuery := c.QueryParam("limit")

	var err error

	limit := 500
	if limitQuery != "" {
		limit, err = strconv.Atoi(limitQuery)
		if err != nil || limit < 1 || limit > 1000 {
			return c.JSON(http.StatusBadRequest, xrpc.XRPCError{ErrStr: "BadRequest", Message: fmt.Sprintf("limit parameter invalid: %s", limitQuery)})
		}
	}

	cursor := int64(0)
	if cursorQuery != "" {
		cursor, err = strconv.ParseInt(cursorQuery, 10, 64)
		if err != nil || cursor < 0 {
			return c.JSON(http.StatusBadRequest, xrpc.XRPCError{ErrStr: "BadRequest", Message: fmt.Sprintf("limit parameter invalid cursor: %s", cursorQuery)})
		}
	}

	out, handleErr := s.handleComAtprotoSyncListRepos(c, cursor, limit)
	if handleErr != nil || out == nil {
		return handleErr
	}
	return c.JSON(200, out)
}

// does a simple HTTP redirect to getRepo on the account's PDS.
//
// NOTE: currently does not check account status locally; a takendown account will still redirect. this saves a database lookup.
func (s *Service) HandleComAtprotoSyncGetRepo(c echo.Context) error {
	ctx, span := otel.Tracer("server").Start(c.Request().Context(), "HandleComAtprotoSyncGetRepo")
	defer span.End()

	didQuery := c.QueryParam("did")

	did, err := syntax.ParseDID(didQuery)
	if err != nil {
		return c.JSON(http.StatusBadRequest, xrpc.XRPCError{ErrStr: "BadRequest", Message: fmt.Sprintf("missing or invalid DID parameter: %s", err)})
	}

	ident, err := s.relay.Dir.LookupDID(ctx, did)
	if err != nil {
		// TODO: could handle lookup errors more granularly
		return c.JSON(http.StatusNotFound, xrpc.XRPCError{ErrStr: "RepoNotFound", Message: fmt.Sprintf("could not resolve DID: %s", err)})
	}
	pdsHost, _, err := relay.ParseHostname(ident.PDSEndpoint())
	if err != nil {
		return c.JSON(http.StatusNotFound, xrpc.XRPCError{ErrStr: "RepoNotFound", Message: "DID document has no valid atproto PDS endpoint"})
	}

	u := c.Request().URL
	if u == nil {
		return fmt.Errorf("unexpected nil URL on request")
	}
	u.Host = pdsHost
	// require SSL for redirect
	u.Scheme = "https"
	// StatusFound is HTTP 302, a temporary redirect
	return c.Redirect(http.StatusFound, u.String())
}

func (s *Service) HandleComAtprotoSyncGetRepoStatus(c echo.Context) error {
	_, span := otel.Tracer("server").Start(c.Request().Context(), "HandleComAtprotoSyncGetRepoStatus")
	defer span.End()

	didQuery := c.QueryParam("did")

	did, err := syntax.ParseDID(didQuery)
	if err != nil {
		return c.JSON(http.StatusBadRequest, xrpc.XRPCError{ErrStr: "BadRequest", Message: fmt.Sprintf("missing or invalid DID parameter: %s", err)})
	}

	out, handleErr := s.handleComAtprotoSyncGetRepoStatus(c, did)
	if handleErr != nil || out == nil {
		return handleErr
	}
	return c.JSON(200, out)
}

func (s *Service) HandleComAtprotoSyncGetLatestCommit(c echo.Context) error {
	_, span := otel.Tracer("server").Start(c.Request().Context(), "HandleComAtprotoSyncGetLatestCommit")
	defer span.End()

	didQuery := c.QueryParam("did")

	did, err := syntax.ParseDID(didQuery)
	if err != nil {
		return c.JSON(http.StatusBadRequest, xrpc.XRPCError{ErrStr: "BadRequest", Message: fmt.Sprintf("missing or invalid DID parameter: %s", err)})
	}

	var out *comatproto.SyncGetLatestCommit_Output
	var handleErr error
	// func (s *Service) handleComAtprotoSyncGetLatestCommit(ctx context.Context,did string) (*comatproto.SyncGetLatestCommit_Output, error)
	out, handleErr = s.handleComAtprotoSyncGetLatestCommit(c, did)
	if handleErr != nil || out == nil {
		return handleErr
	}
	return c.JSON(200, out)
}
