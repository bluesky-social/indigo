package labeling

import (
	"io"
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
	//e.GET("/xrpc/com.atproto.account.get", s.HandleComAtprotoAccountGet)

	// label-specific
	e.GET("/xrpc/com.atproto.label.query", s.HandleComAtprotoLabelQuery)

	// minimal moderation reporting/actioning
	e.GET("/xrpc/com.atproto.admin.getModerationAction", s.HandleComAtprotoAdminGetModerationAction)
	e.GET("/xrpc/com.atproto.admin.getModerationActions", s.HandleComAtprotoAdminGetModerationActions)
	e.GET("/xrpc/com.atproto.admin.getModerationReport", s.HandleComAtprotoAdminGetModerationReport)
	e.GET("/xrpc/com.atproto.admin.getModerationReports", s.HandleComAtprotoAdminGetModerationReports)
	e.POST("/xrpc/com.atproto.admin.resolveModerationReports", s.HandleComAtprotoAdminResolveModerationReports)
	e.POST("/xrpc/com.atproto.admin.reverseModerationAction", s.HandleComAtprotoAdminReverseModerationAction)
	e.POST("/xrpc/com.atproto.admin.takeModerationAction", s.HandleComAtprotoAdminTakeModerationAction)
	e.POST("/xrpc/com.atproto.report.create", s.HandleComAtprotoReportCreate)
	// TODO: proxy the rest to BGS?
	//e.GET("/xrpc/com.atproto.admin.getRecord", s.HandleComAtprotoAdminGetRecord)
	//e.GET("/xrpc/com.atproto.admin.getRepo", s.HandleComAtprotoAdminGetRepo)
	//e.GET("/xrpc/com.atproto.admin.searchRepos", s.HandleComAtprotoAdminSearchRepos)

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

func (s *Server) HandleComAtprotoRepoListRecords(c echo.Context) error {
	ctx, span := otel.Tracer("server").Start(c.Request().Context(), "HandleComAtprotoRepoListRecords")
	defer span.End()
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
	repo := c.QueryParam("repo")

	var reverse *bool
	if p := c.QueryParam("reverse"); p != "" {
		reverse_val, err := strconv.ParseBool(p)
		if err != nil {
			return err
		}
		reverse = &reverse_val
	}
	rkeyEnd := c.QueryParam("rkeyEnd")
	rkeyStart := c.QueryParam("rkeyStart")
	var out *atproto.RepoListRecords_Output
	var handleErr error
	// func (s *Server) handleComAtprotoRepoListRecords(ctx context.Context,collection string,limit int,repo string,reverse *bool,rkeyEnd string,rkeyStart string) (*comatprototypes.RepoListRecords_Output, error)
	out, handleErr = s.handleComAtprotoRepoListRecords(ctx, collection, limit, repo, reverse, rkeyEnd, rkeyStart)
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
	var out *atproto.AdminModerationAction_ViewDetail
	var handleErr error
	// func (s *Server) handleComAtprotoAdminGetModerationAction(ctx context.Context,id int) (*atproto.AdminModerationAction_ViewDetail, error)
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
	var out *atproto.AdminModerationReport_ViewDetail
	var handleErr error
	// func (s *Server) handleComAtprotoAdminGetModerationReport(ctx context.Context,id int) (*atproto.AdminModerationReport_ViewDetail, error)
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
	var out *atproto.AdminModerationAction_View
	var handleErr error
	// func (s *Server) handleComAtprotoAdminResolveModerationReports(ctx context.Context,body *atproto.AdminResolveModerationReports_Input) (*atproto.AdminModerationAction_View, error)
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
	var out *atproto.AdminModerationAction_View
	var handleErr error
	// func (s *Server) handleComAtprotoAdminReverseModerationAction(ctx context.Context,body *atproto.AdminReverseModerationAction_Input) (*atproto.AdminModerationAction_View, error)
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
	var out *atproto.AdminModerationAction_View
	var handleErr error
	// func (s *Server) handleComAtprotoAdminTakeModerationAction(ctx context.Context,body *atproto.AdminTakeModerationAction_Input) (*atproto.AdminModerationAction_View, error)
	out, handleErr = s.handleComAtprotoAdminTakeModerationAction(ctx, &body)
	if handleErr != nil {
		return handleErr
	}
	return c.JSON(200, out)
}

func (s *Server) HandleComAtprotoReportCreate(c echo.Context) error {
	ctx, span := otel.Tracer("server").Start(c.Request().Context(), "HandleComAtprotoReportCreate")
	defer span.End()

	var body atproto.ReportCreate_Input
	if err := c.Bind(&body); err != nil {
		return err
	}
	var out *atproto.ReportCreate_Output
	var handleErr error
	// func (s *Server) handleComAtprotoReportCreate(ctx context.Context,body *atproto.ReportCreate_Input) (*atproto.ReportCreate_Output, error)
	out, handleErr = s.handleComAtprotoReportCreate(ctx, &body)
	if handleErr != nil {
		return handleErr
	}
	return c.JSON(200, out)
}
