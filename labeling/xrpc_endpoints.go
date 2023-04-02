package labeling

import (
	"net/http/httputil"
	"strconv"

	atproto "github.com/bluesky-social/indigo/api/atproto"
	label "github.com/bluesky-social/indigo/api/label"

	"github.com/labstack/echo/v4"
	"go.opentelemetry.io/otel"
)

func (s *Server) RegisterHandlersComAtproto(e *echo.Echo) error {
	// handle/account hosting
	e.GET("/xrpc/com.atproto.identity.resolveHandle", s.HandleComAtprotoIdentityResolveHandle)
	e.GET("/xrpc/com.atproto.server.describeServer", s.HandleComAtprotoServerDescribeServer)
	// TODO: session create/refresh/delete?

	// minimal moderation reporting/actioning
	e.GET("/xrpc/com.atproto.admin.getModerationAction", s.HandleComAtprotoAdminGetModerationAction)
	e.GET("/xrpc/com.atproto.admin.getModerationActions", s.HandleComAtprotoAdminGetModerationActions)
	e.GET("/xrpc/com.atproto.admin.getModerationReport", s.HandleComAtprotoAdminGetModerationReport)
	e.GET("/xrpc/com.atproto.admin.getModerationReports", s.HandleComAtprotoAdminGetModerationReports)
	e.POST("/xrpc/com.atproto.admin.resolveModerationReports", s.HandleComAtprotoAdminResolveModerationReports)
	e.POST("/xrpc/com.atproto.admin.reverseModerationAction", s.HandleComAtprotoAdminReverseModerationAction)
	e.POST("/xrpc/com.atproto.admin.takeModerationAction", s.HandleComAtprotoAdminTakeModerationAction)
	e.POST("/xrpc/com.atproto.report.create", s.HandleComAtprotoReportCreate)

	// label-specific
	e.GET("/xrpc/com.atproto.label.query", s.HandleComAtprotoLabelQuery)

	return nil
}

func (s *Server) rewriteProxyRequestAdmin(r *httputil.ProxyRequest) {
	r.SetXForwarded()
	r.SetURL(s.xrpcProxyURL)
	r.Out.Header.Set("Authorization", s.xrpcProxyAuthHeader)
}

func (s *Server) RegisterProxyHandlers(e *echo.Echo) error {

	rp := &httputil.ReverseProxy{Rewrite: s.rewriteProxyRequestAdmin}

	// Proxy some admin requests
	e.GET("/xrpc/com.atproto.admin.getRecord", echo.WrapHandler(rp))
	e.GET("/xrpc/com.atproto.admin.getRepo", echo.WrapHandler(rp))
	e.GET("/xrpc/com.atproto.admin.searchRepos", echo.WrapHandler(rp))

	return nil
}

func (s *Server) HandleComAtprotoIdentityResolveHandle(c echo.Context) error {
	ctx, span := otel.Tracer("server").Start(c.Request().Context(), "HandleComAtprotoIdentityResolveHandle")
	defer span.End()
	handle := c.QueryParam("handle")
	var out *atproto.IdentityResolveHandle_Output
	var handleErr error
	// func (s *Server) handleComAtprotoIdentityResolveHandle(ctx context.Context,handle string) (*atproto.IdentityResolveHandle_Output, error)
	out, handleErr = s.handleComAtprotoIdentityResolveHandle(ctx, handle)
	if handleErr != nil {
		return handleErr
	}
	return c.JSON(200, out)
}

func (s *Server) HandleComAtprotoRepoDescribeRepo(c echo.Context) error {
	ctx, span := otel.Tracer("server").Start(c.Request().Context(), "HandleComAtprotoRepoDescribeRepo")
	defer span.End()
	repo := c.QueryParam("repo")
	var out *atproto.RepoDescribeRepo_Output
	var handleErr error
	// func (s *Server) handleComAtprotoRepoDescribeRepo(ctx context.Context,repo string) (*atproto.RepoDescribeRepo_Output, error)
	out, handleErr = s.handleComAtprotoRepoDescribeRepo(ctx, repo)
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
	repo := c.QueryParam("repo")
	rkey := c.QueryParam("rkey")
	var out *atproto.RepoGetRecord_Output
	var handleErr error
	// func (s *Server) handleComAtprotoRepoGetRecord(ctx context.Context,cid string,collection string,repo string,rkey string) (*atproto.RepoGetRecord_Output, error)
	out, handleErr = s.handleComAtprotoRepoGetRecord(ctx, cid, collection, repo, rkey)
	if handleErr != nil {
		return handleErr
	}
	return c.JSON(200, out)
}

func (s *Server) HandleComAtprotoServerDescribeServer(c echo.Context) error {
	ctx, span := otel.Tracer("server").Start(c.Request().Context(), "HandleComAtprotoServerDescribeServer")
	defer span.End()
	var out *atproto.ServerDescribeServer_Output
	var handleErr error
	// func (s *Server) handleComAtprotoServerDescribeServer(ctx context.Context) (*atproto.ServerDescribeServer_Output, error)
	out, handleErr = s.handleComAtprotoServerDescribeServer(ctx)
	if handleErr != nil {
		return handleErr
	}
	return c.JSON(200, out)
}

func (s *Server) HandleComAtprotoLabelQuery(c echo.Context) error {
	ctx, span := otel.Tracer("server").Start(c.Request().Context(), "HandleComAtprotoLabelQuery")
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
		limit = 20
	}

	sources := c.QueryParams()["sources"]

	uriPatterns := c.QueryParams()["uriPatterns"]
	var out *label.Query_Output
	var handleErr error
	// func (s *Server) handleComAtprotoLabelQuery(ctx context.Context,cursor string,limit int,sources []string,uriPatterns []string) (*comatprototypes.LabelQuery_Output, error)
	out, handleErr = s.handleComAtprotoLabelQuery(ctx, cursor, limit, sources, uriPatterns)
	if handleErr != nil {
		return handleErr
	}
	return c.JSON(200, out)
}

func (s *Server) HandleComAtprotoAdminGetModerationAction(c echo.Context) error {
	ctx, span := otel.Tracer("server").Start(c.Request().Context(), "HandleComAtprotoAdminGetModerationAction")
	defer span.End()

	id, err := strconv.Atoi(c.QueryParam("id"))
	if err != nil {
		return err
	}
	var out *atproto.AdminDefs_ActionViewDetail
	var handleErr error
	// func (s *Server) handleComAtprotoAdminGetModerationAction(ctx context.Context,id int) (*atproto.AdminDefs_ActionViewDetail, error)
	out, handleErr = s.handleComAtprotoAdminGetModerationAction(ctx, id)
	if handleErr != nil {
		return handleErr
	}
	return c.JSON(200, out)
}

func (s *Server) HandleComAtprotoAdminGetModerationActions(c echo.Context) error {
	ctx, span := otel.Tracer("server").Start(c.Request().Context(), "HandleComAtprotoAdminGetModerationActions")
	defer span.End()
	before := c.QueryParam("before")

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
	subject := c.QueryParam("subject")
	var out *atproto.AdminGetModerationActions_Output
	var handleErr error
	// func (s *Server) handleComAtprotoAdminGetModerationActions(ctx context.Context,before string,limit int,subject string) (*atproto.AdminGetModerationActions_Output, error)
	out, handleErr = s.handleComAtprotoAdminGetModerationActions(ctx, before, limit, subject)
	if handleErr != nil {
		return handleErr
	}
	return c.JSON(200, out)
}

func (s *Server) HandleComAtprotoAdminGetModerationReport(c echo.Context) error {
	ctx, span := otel.Tracer("server").Start(c.Request().Context(), "HandleComAtprotoAdminGetModerationReport")
	defer span.End()

	id, err := strconv.Atoi(c.QueryParam("id"))
	if err != nil {
		return err
	}
	var out *atproto.AdminDefs_ReportViewDetail
	var handleErr error
	// func (s *Server) handleComAtprotoAdminGetModerationReport(ctx context.Context,id int) (*atproto.AdminDefs_ReportViewDetail, error)
	out, handleErr = s.handleComAtprotoAdminGetModerationReport(ctx, id)
	if handleErr != nil {
		return handleErr
	}
	return c.JSON(200, out)
}

func (s *Server) HandleComAtprotoAdminGetModerationReports(c echo.Context) error {
	ctx, span := otel.Tracer("server").Start(c.Request().Context(), "HandleComAtprotoAdminGetModerationReports")
	defer span.End()
	before := c.QueryParam("before")

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

	var resolved *bool
	if p := c.QueryParam("resolved"); p != "" {
		resolved_val, err := strconv.ParseBool(p)
		if err != nil {
			return err
		}
		resolved = &resolved_val
	}
	subject := c.QueryParam("subject")
	var out *atproto.AdminGetModerationReports_Output
	var handleErr error
	// func (s *Server) handleComAtprotoAdminGetModerationReports(ctx context.Context,before string,limit int,resolved *bool,subject string) (*atproto.AdminGetModerationReports_Output, error)
	out, handleErr = s.handleComAtprotoAdminGetModerationReports(ctx, before, limit, resolved, subject)
	if handleErr != nil {
		return handleErr
	}
	return c.JSON(200, out)
}

func (s *Server) HandleComAtprotoAdminResolveModerationReports(c echo.Context) error {
	ctx, span := otel.Tracer("server").Start(c.Request().Context(), "HandleComAtprotoAdminResolveModerationReports")
	defer span.End()

	var body atproto.AdminResolveModerationReports_Input
	if err := c.Bind(&body); err != nil {
		return err
	}
	var out *atproto.AdminDefs_ActionView
	var handleErr error
	// func (s *Server) handleComAtprotoAdminResolveModerationReports(ctx context.Context,body *atproto.AdminResolveModerationReports_Input) (*atproto.AdminDefs_ActionView, error)
	out, handleErr = s.handleComAtprotoAdminResolveModerationReports(ctx, &body)
	if handleErr != nil {
		return handleErr
	}
	return c.JSON(200, out)
}

func (s *Server) HandleComAtprotoAdminReverseModerationAction(c echo.Context) error {
	ctx, span := otel.Tracer("server").Start(c.Request().Context(), "HandleComAtprotoAdminReverseModerationAction")
	defer span.End()

	var body atproto.AdminReverseModerationAction_Input
	if err := c.Bind(&body); err != nil {
		return err
	}
	var out *atproto.AdminDefs_ActionView
	var handleErr error
	// func (s *Server) handleComAtprotoAdminReverseModerationAction(ctx context.Context,body *atproto.AdminReverseModerationAction_Input) (*atproto.AdminDefs_ActionView, error)
	out, handleErr = s.handleComAtprotoAdminReverseModerationAction(ctx, &body)
	if handleErr != nil {
		return handleErr
	}
	return c.JSON(200, out)
}

func (s *Server) HandleComAtprotoAdminTakeModerationAction(c echo.Context) error {
	ctx, span := otel.Tracer("server").Start(c.Request().Context(), "HandleComAtprotoAdminTakeModerationAction")
	defer span.End()

	var body atproto.AdminTakeModerationAction_Input
	if err := c.Bind(&body); err != nil {
		return err
	}
	var out *atproto.AdminDefs_ActionView
	var handleErr error
	// func (s *Server) handleComAtprotoAdminTakeModerationAction(ctx context.Context,body *atproto.AdminTakeModerationAction_Input) (*atproto.AdminDefs_ActionView, error)
	out, handleErr = s.handleComAtprotoAdminTakeModerationAction(ctx, &body)
	if handleErr != nil {
		return handleErr
	}
	return c.JSON(200, out)
}

func (s *Server) HandleComAtprotoReportCreate(c echo.Context) error {
	ctx, span := otel.Tracer("server").Start(c.Request().Context(), "HandleComAtprotoReportCreate")
	defer span.End()

	var body atproto.ModerationCreateReport_Input
	if err := c.Bind(&body); err != nil {
		return err
	}
	var out *atproto.ModerationCreateReport_Output
	var handleErr error
	// func (s *Server) handleComAtprotoReportCreate(ctx context.Context,body *atproto.ModerationCreateReport_Input) (*atproto.ModerationCreateReport_Output, error)
	out, handleErr = s.handleComAtprotoReportCreate(ctx, &body)
	if handleErr != nil {
		return handleErr
	}
	return c.JSON(200, out)
}
