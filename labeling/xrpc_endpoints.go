package labeling

import (
	"io"
	"strconv"

	atproto "github.com/bluesky-social/indigo/api/atproto"

	"github.com/labstack/echo/v4"
	"go.opentelemetry.io/otel"
)

func (s *Server) RegisterHandlersComAtproto(e *echo.Echo) error {
	e.GET("/xrpc/com.atproto.account.get", s.HandleComAtprotoAccountGet)
	e.GET("/xrpc/com.atproto.handle.resolve", s.HandleComAtprotoHandleResolve)
	e.GET("/xrpc/com.atproto.repo.describe", s.HandleComAtprotoRepoDescribe)
	e.GET("/xrpc/com.atproto.repo.getRecord", s.HandleComAtprotoRepoGetRecord)
	e.GET("/xrpc/com.atproto.repo.listRecords", s.HandleComAtprotoRepoListRecords)
	e.GET("/xrpc/com.atproto.server.getAccountsConfig", s.HandleComAtprotoServerGetAccountsConfig)
	e.GET("/xrpc/com.atproto.sync.getHead", s.HandleComAtprotoSyncGetHead)
	return nil
}

func (s *Server) HandleComAtprotoAccountGet(c echo.Context) error {
	ctx, span := otel.Tracer("server").Start(c.Request().Context(), "HandleComAtprotoAccountGet")
	defer span.End()
	var handleErr error
	// func (s *Server) handleComAtprotoAccountGet(ctx context.Context) error
	handleErr = s.handleComAtprotoAccountGet(ctx)
	if handleErr != nil {
		return handleErr
	}
	return nil
}

func (s *Server) HandleComAtprotoHandleResolve(c echo.Context) error {
	ctx, span := otel.Tracer("server").Start(c.Request().Context(), "HandleComAtprotoHandleResolve")
	defer span.End()
	handle := c.QueryParam("handle")
	var out *atproto.HandleResolve_Output
	var handleErr error
	// func (s *Server) handleComAtprotoHandleResolve(ctx context.Context,handle string) (*atproto.HandleResolve_Output, error)
	out, handleErr = s.handleComAtprotoHandleResolve(ctx, handle)
	if handleErr != nil {
		return handleErr
	}
	return c.JSON(200, out)
}

func (s *Server) HandleComAtprotoRepoDescribe(c echo.Context) error {
	ctx, span := otel.Tracer("server").Start(c.Request().Context(), "HandleComAtprotoRepoDescribe")
	defer span.End()
	user := c.QueryParam("user")
	var out *atproto.RepoDescribe_Output
	var handleErr error
	// func (s *Server) handleComAtprotoRepoDescribe(ctx context.Context,user string) (*atproto.RepoDescribe_Output, error)
	out, handleErr = s.handleComAtprotoRepoDescribe(ctx, user)
	if handleErr != nil {
		return handleErr
	}
	return c.JSON(200, out)
}

func (s *Server) HandleComAtprotoRepoGetRecord(c echo.Context) error {
	ctx, span := otel.Tracer("server").Start(c.Request().Context(), "HandleComAtprotoRepoGetRecord")
	defer span.End()
	cid := c.QueryParam("cid")
	collection := c.QueryParam("collection")
	rkey := c.QueryParam("rkey")
	user := c.QueryParam("user")
	var out *atproto.RepoGetRecord_Output
	var handleErr error
	// func (s *Server) handleComAtprotoRepoGetRecord(ctx context.Context,cid string,collection string,rkey string,user string) (*atproto.RepoGetRecord_Output, error)
	out, handleErr = s.handleComAtprotoRepoGetRecord(ctx, cid, collection, rkey, user)
	if handleErr != nil {
		return handleErr
	}
	return c.JSON(200, out)
}

func (s *Server) HandleComAtprotoRepoListRecords(c echo.Context) error {
	ctx, span := otel.Tracer("server").Start(c.Request().Context(), "HandleComAtprotoRepoListRecords")
	defer span.End()
	after := c.QueryParam("after")
	before := c.QueryParam("before")
	collection := c.QueryParam("collection")

	var limit int
	if p := c.QueryParam("limit"); p != "" {
		var err error
		limit, err = strconv.Atoi(p)
		if err != nil {
			return err
		}
	} else {
		limit = 50
	}

	var reverse *bool
	if p := c.QueryParam("reverse"); p != "" {
		reverse_val, err := strconv.ParseBool(p)
		if err != nil {
			return err
		}
		reverse = &reverse_val
	}
	user := c.QueryParam("user")
	var out *atproto.RepoListRecords_Output
	var handleErr error
	// func (s *Server) handleComAtprotoRepoListRecords(ctx context.Context,after string,before string,collection string,limit int,reverse *bool,user string) (*atproto.RepoListRecords_Output, error)
	out, handleErr = s.handleComAtprotoRepoListRecords(ctx, after, before, collection, limit, reverse, user)
	if handleErr != nil {
		return handleErr
	}
	return c.JSON(200, out)
}

func (s *Server) HandleComAtprotoServerGetAccountsConfig(c echo.Context) error {
	ctx, span := otel.Tracer("server").Start(c.Request().Context(), "HandleComAtprotoServerGetAccountsConfig")
	defer span.End()
	var out *atproto.ServerGetAccountsConfig_Output
	var handleErr error
	// func (s *Server) handleComAtprotoServerGetAccountsConfig(ctx context.Context) (*atproto.ServerGetAccountsConfig_Output, error)
	out, handleErr = s.handleComAtprotoServerGetAccountsConfig(ctx)
	if handleErr != nil {
		return handleErr
	}
	return c.JSON(200, out)
}

func (s *Server) HandleComAtprotoSyncGetRepo(c echo.Context) error {
	ctx, span := otel.Tracer("server").Start(c.Request().Context(), "HandleComAtprotoSyncGetRepo")
	defer span.End()
	did := c.QueryParam("did")
	earliest := c.QueryParam("earliest")
	latest := c.QueryParam("latest")
	var out io.Reader
	var handleErr error
	// func (s *Server) handleComAtprotoSyncGetRepo(ctx context.Context,did string,earliest string,latest string) (io.Reader, error)
	out, handleErr = s.handleComAtprotoSyncGetRepo(ctx, did, earliest, latest)
	if handleErr != nil {
		return handleErr
	}
	return c.Stream(200, "application/vnd.ipld.car", out)
}

func (s *Server) HandleComAtprotoSyncGetHead(c echo.Context) error {
	ctx, span := otel.Tracer("server").Start(c.Request().Context(), "HandleComAtprotoSyncGetHead")
	defer span.End()
	did := c.QueryParam("did")
	var out *atproto.SyncGetHead_Output
	var handleErr error
	// func (s *Server) handleComAtprotoSyncGetHead(ctx context.Context,did string) (*comatprototypes.SyncGetHead_Output, error)
	out, handleErr = s.handleComAtprotoSyncGetHead(ctx, did)
	if handleErr != nil {
		return handleErr
	}
	return c.JSON(200, out)
}
