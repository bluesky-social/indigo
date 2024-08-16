package bgs

import (
	"fmt"
	"io"
	"net/http"
	"strconv"

	comatprototypes "github.com/bluesky-social/indigo/api/atproto"
	"github.com/bluesky-social/indigo/atproto/syntax"
	"github.com/ipfs/go-cid"
	"github.com/labstack/echo/v4"
	"go.opentelemetry.io/otel"
)

type XRPCError struct {
	Message string `json:"message"`
}

func (s *BGS) RegisterHandlersAppBsky(e *echo.Echo) error {
	return nil
}

func (s *BGS) RegisterHandlersComAtproto(e *echo.Echo) error {
	e.GET("/xrpc/com.atproto.sync.getBlocks", s.HandleComAtprotoSyncGetBlocks)
	e.GET("/xrpc/com.atproto.sync.getLatestCommit", s.HandleComAtprotoSyncGetLatestCommit)
	e.GET("/xrpc/com.atproto.sync.getRecord", s.HandleComAtprotoSyncGetRecord)
	e.GET("/xrpc/com.atproto.sync.getRepo", s.HandleComAtprotoSyncGetRepo)
	e.GET("/xrpc/com.atproto.sync.listRepos", s.HandleComAtprotoSyncListRepos)
	e.POST("/xrpc/com.atproto.sync.notifyOfUpdate", s.HandleComAtprotoSyncNotifyOfUpdate)
	e.POST("/xrpc/com.atproto.sync.requestCrawl", s.HandleComAtprotoSyncRequestCrawl)
	return nil
}

func (s *BGS) HandleComAtprotoSyncGetBlocks(c echo.Context) error {
	ctx, span := otel.Tracer("server").Start(c.Request().Context(), "HandleComAtprotoSyncGetBlocks")
	defer span.End()

	cids := c.QueryParams()["cids"]
	did := c.QueryParam("did")
	_, err := syntax.ParseDID(did)
	if err != nil {
		return c.JSON(http.StatusBadRequest, XRPCError{Message: fmt.Sprintf("invalid did: %s", did)})
	}

	for _, cd := range cids {
		_, err = cid.Parse(cd)
		if err != nil {
			return c.JSON(http.StatusBadRequest, XRPCError{Message: fmt.Sprintf("invalid cid: %s", cd)})
		}
	}

	var out io.Reader
	var handleErr error
	// func (s *BGS) handleComAtprotoSyncGetBlocks(ctx context.Context,cids []string,did string) (io.Reader, error)
	out, handleErr = s.handleComAtprotoSyncGetBlocks(ctx, cids, did)
	if handleErr != nil {
		return handleErr
	}
	return c.Stream(200, "application/vnd.ipld.car", out)
}

func (s *BGS) HandleComAtprotoSyncGetLatestCommit(c echo.Context) error {
	ctx, span := otel.Tracer("server").Start(c.Request().Context(), "HandleComAtprotoSyncGetLatestCommit")
	defer span.End()
	did := c.QueryParam("did")

	_, err := syntax.ParseDID(did)
	if err != nil {
		return c.JSON(http.StatusBadRequest, XRPCError{Message: fmt.Sprintf("invalid did: %s", did)})
	}

	var out *comatprototypes.SyncGetLatestCommit_Output
	var handleErr error
	// func (s *BGS) handleComAtprotoSyncGetLatestCommit(ctx context.Context,did string) (*comatprototypes.SyncGetLatestCommit_Output, error)
	out, handleErr = s.handleComAtprotoSyncGetLatestCommit(ctx, did)
	if handleErr != nil {
		return handleErr
	}
	return c.JSON(200, out)
}

func (s *BGS) HandleComAtprotoSyncGetRecord(c echo.Context) error {
	ctx, span := otel.Tracer("server").Start(c.Request().Context(), "HandleComAtprotoSyncGetRecord")
	defer span.End()
	collection := c.QueryParam("collection")
	did := c.QueryParam("did")
	rkey := c.QueryParam("rkey")

	_, err := syntax.ParseRecordKey(rkey)
	if err != nil {
		return c.JSON(http.StatusBadRequest, XRPCError{Message: fmt.Sprintf("invalid rkey: %s", rkey)})
	}

	_, err = syntax.ParseNSID(collection)
	if err != nil {
		return c.JSON(http.StatusBadRequest, XRPCError{Message: fmt.Sprintf("invalid collection: %s", collection)})
	}

	_, err = syntax.ParseDID(did)
	if err != nil {
		return c.JSON(http.StatusBadRequest, XRPCError{Message: fmt.Sprintf("invalid did: %s", did)})
	}

	var out io.Reader
	var handleErr error
	// func (s *BGS) handleComAtprotoSyncGetRecord(ctx context.Context,collection string,commit string,did string,rkey string) (io.Reader, error)
	out, handleErr = s.handleComAtprotoSyncGetRecord(ctx, collection, did, rkey)
	if handleErr != nil {
		return handleErr
	}
	return c.Stream(200, "application/vnd.ipld.car", out)
}

func (s *BGS) HandleComAtprotoSyncGetRepo(c echo.Context) error {
	ctx, span := otel.Tracer("server").Start(c.Request().Context(), "HandleComAtprotoSyncGetRepo")
	defer span.End()
	did := c.QueryParam("did")
	since := c.QueryParam("since")

	_, err := syntax.ParseDID(did)
	if err != nil {
		return c.JSON(http.StatusBadRequest, XRPCError{Message: fmt.Sprintf("invalid did: %s", did)})
	}

	var out io.Reader
	var handleErr error
	// func (s *BGS) handleComAtprotoSyncGetRepo(ctx context.Context,did string,since string) (io.Reader, error)
	out, handleErr = s.handleComAtprotoSyncGetRepo(ctx, did, since)
	if handleErr != nil {
		return handleErr
	}
	return c.Stream(200, "application/vnd.ipld.car", out)
}

func (s *BGS) HandleComAtprotoSyncListRepos(c echo.Context) error {
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

func (s *BGS) HandleComAtprotoSyncNotifyOfUpdate(c echo.Context) error {
	ctx, span := otel.Tracer("server").Start(c.Request().Context(), "HandleComAtprotoSyncNotifyOfUpdate")
	defer span.End()

	var body comatprototypes.SyncNotifyOfUpdate_Input
	if err := c.Bind(&body); err != nil {
		return c.JSON(http.StatusBadRequest, XRPCError{Message: fmt.Sprintf("invalid body: %s", err)})
	}
	var handleErr error
	// func (s *BGS) handleComAtprotoSyncNotifyOfUpdate(ctx context.Context,body *comatprototypes.SyncNotifyOfUpdate_Input) error
	handleErr = s.handleComAtprotoSyncNotifyOfUpdate(ctx, &body)
	if handleErr != nil {
		return handleErr
	}
	return nil
}

func (s *BGS) HandleComAtprotoSyncRequestCrawl(c echo.Context) error {
	ctx, span := otel.Tracer("server").Start(c.Request().Context(), "HandleComAtprotoSyncRequestCrawl")
	defer span.End()

	var body comatprototypes.SyncRequestCrawl_Input
	if err := c.Bind(&body); err != nil {
		return c.JSON(http.StatusBadRequest, XRPCError{Message: fmt.Sprintf("invalid body: %s", err)})
	}
	var handleErr error
	// func (s *BGS) handleComAtprotoSyncRequestCrawl(ctx context.Context,body *comatprototypes.SyncRequestCrawl_Input) error
	handleErr = s.handleComAtprotoSyncRequestCrawl(ctx, &body)
	if handleErr != nil {
		return handleErr
	}
	return nil
}
