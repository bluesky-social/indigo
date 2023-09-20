package bgs

import (
	"io"
	"strconv"

	comatprototypes "github.com/bluesky-social/indigo/api/atproto"
	"github.com/labstack/echo/v4"
	"go.opentelemetry.io/otel"
)

func (s *BGS) RegisterHandlersAppBsky(e *echo.Echo) error {
	return nil
}

func (s *BGS) RegisterHandlersComAtproto(e *echo.Echo) error {
	e.GET("/xrpc/com.atproto.sync.getBlob", s.HandleComAtprotoSyncGetBlob)
	e.GET("/xrpc/com.atproto.sync.getBlocks", s.HandleComAtprotoSyncGetBlocks)
	e.GET("/xrpc/com.atproto.sync.getLatestCommit", s.HandleComAtprotoSyncGetLatestCommit)
	e.GET("/xrpc/com.atproto.sync.getRecord", s.HandleComAtprotoSyncGetRecord)
	e.GET("/xrpc/com.atproto.sync.getRepo", s.HandleComAtprotoSyncGetRepo)
	e.GET("/xrpc/com.atproto.sync.listBlobs", s.HandleComAtprotoSyncListBlobs)
	e.GET("/xrpc/com.atproto.sync.listRepos", s.HandleComAtprotoSyncListRepos)
	e.POST("/xrpc/com.atproto.sync.notifyOfUpdate", s.HandleComAtprotoSyncNotifyOfUpdate)
	e.POST("/xrpc/com.atproto.sync.requestCrawl", s.HandleComAtprotoSyncRequestCrawl)
	return nil
}

func (s *BGS) HandleComAtprotoSyncGetBlob(c echo.Context) error {
	ctx, span := otel.Tracer("server").Start(c.Request().Context(), "HandleComAtprotoSyncGetBlob")
	defer span.End()
	cid := c.QueryParam("cid")
	did := c.QueryParam("did")
	var out io.Reader
	var handleErr error
	// func (s *BGS) handleComAtprotoSyncGetBlob(ctx context.Context,cid string,did string) (io.Reader, error)
	out, handleErr = s.handleComAtprotoSyncGetBlob(ctx, cid, did)
	if handleErr != nil {
		return handleErr
	}
	return c.Stream(200, "application/octet-stream", out)
}

func (s *BGS) HandleComAtprotoSyncGetBlocks(c echo.Context) error {
	ctx, span := otel.Tracer("server").Start(c.Request().Context(), "HandleComAtprotoSyncGetBlocks")
	defer span.End()

	cids := c.QueryParams()["cids"]
	did := c.QueryParam("did")
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
	commit := c.QueryParam("commit")
	did := c.QueryParam("did")
	rkey := c.QueryParam("rkey")
	var out io.Reader
	var handleErr error
	// func (s *BGS) handleComAtprotoSyncGetRecord(ctx context.Context,collection string,commit string,did string,rkey string) (io.Reader, error)
	out, handleErr = s.handleComAtprotoSyncGetRecord(ctx, collection, commit, did, rkey)
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
	var out io.Reader
	var handleErr error
	// func (s *BGS) handleComAtprotoSyncGetRepo(ctx context.Context,did string,since string) (io.Reader, error)
	out, handleErr = s.handleComAtprotoSyncGetRepo(ctx, did, since)
	if handleErr != nil {
		return handleErr
	}
	return c.Stream(200, "application/vnd.ipld.car", out)
}

func (s *BGS) HandleComAtprotoSyncListBlobs(c echo.Context) error {
	ctx, span := otel.Tracer("server").Start(c.Request().Context(), "HandleComAtprotoSyncListBlobs")
	defer span.End()
	cursor := c.QueryParam("cursor")
	did := c.QueryParam("did")

	var limit int
	if p := c.QueryParam("limit"); p != "" {
		var err error
		limit, err = strconv.Atoi(p)
		if err != nil {
			return err
		}
	} else {
		limit = 500
	}
	since := c.QueryParam("since")
	var out *comatprototypes.SyncListBlobs_Output
	var handleErr error
	// func (s *BGS) handleComAtprotoSyncListBlobs(ctx context.Context,cursor string,did string,limit int,since string) (*comatprototypes.SyncListBlobs_Output, error)
	out, handleErr = s.handleComAtprotoSyncListBlobs(ctx, cursor, did, limit, since)
	if handleErr != nil {
		return handleErr
	}
	return c.JSON(200, out)
}

func (s *BGS) HandleComAtprotoSyncListRepos(c echo.Context) error {
	ctx, span := otel.Tracer("server").Start(c.Request().Context(), "HandleComAtprotoSyncListRepos")
	defer span.End()
	cursor := c.QueryParam("cursor")

	var limit int
	if p := c.QueryParam("limit"); p != "" {
		var err error
		limit, err = strconv.Atoi(p)
		if err != nil {
			return err
		}
	} else {
		limit = 500
	}
	var out *comatprototypes.SyncListRepos_Output
	var handleErr error
	// func (s *BGS) handleComAtprotoSyncListRepos(ctx context.Context,cursor string,limit int) (*comatprototypes.SyncListRepos_Output, error)
	out, handleErr = s.handleComAtprotoSyncListRepos(ctx, cursor, limit)
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
		return err
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
		return err
	}
	var handleErr error
	// func (s *BGS) handleComAtprotoSyncRequestCrawl(ctx context.Context,body *comatprototypes.SyncRequestCrawl_Input) error
	handleErr = s.handleComAtprotoSyncRequestCrawl(ctx, &body)
	if handleErr != nil {
		return handleErr
	}
	return nil
}
