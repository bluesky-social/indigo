package labeler

import (
	"net/http/httputil"
	"strconv"

	atproto "github.com/bluesky-social/indigo/api/atproto"
	label "github.com/bluesky-social/indigo/api/label"

	"github.com/carlmjohnson/versioninfo"
	"github.com/labstack/echo/v4"
	"go.opentelemetry.io/otel"
)

func (s *Server) RegisterHandlersComAtproto(e *echo.Echo) error {
	// handle/account hosting
	e.GET("/xrpc/com.atproto.server.describeServer", s.HandleComAtprotoServerDescribeServer)
	// TODO: session create/refresh/delete?

	// minimal moderation reporting/actioning
	e.POST("/xrpc/com.atproto.report.create", s.HandleComAtprotoReportCreate)

	// label-specific
	e.GET("/xrpc/com.atproto.label.queryLabels", s.HandleComAtprotoLabelQueryLabels)

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

func (s *Server) HandleComAtprotoLabelQueryLabels(c echo.Context) error {
	ctx, span := otel.Tracer("server").Start(c.Request().Context(), "HandleComAtprotoLabelQueryLabels")
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
	var out *label.QueryLabels_Output
	var handleErr error
	// func (s *Server) handleComAtprotoLabelQueryLabels(ctx context.Context,cursor string,limit int,sources []string,uriPatterns []string) (*comatprototypes.LabelQueryLabels_Output, error)
	out, handleErr = s.handleComAtprotoLabelQueryLabels(ctx, cursor, limit, sources, uriPatterns)
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

type HealthStatus struct {
	Status  string `json:"status"`
	Version string `json:"version"`
	Message string `json:"msg,omitempty"`
}

func (s *Server) HandleHealthCheck(c echo.Context) error {
	if err := s.db.Exec("SELECT 1").Error; err != nil {
		log.Errorf("healthcheck can't connect to database: %v", err)
		return c.JSON(500, HealthStatus{Status: "error", Version: versioninfo.Short(), Message: "can't connect to database"})
	} else {
		return c.JSON(200, HealthStatus{Status: "ok", Version: versioninfo.Short()})
	}
}
