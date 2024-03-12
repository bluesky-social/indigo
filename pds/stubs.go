package pds

import (
	"io"
	"strconv"

	comatprototypes "github.com/bluesky-social/indigo/api/atproto"
	"github.com/labstack/echo/v4"
	"go.opentelemetry.io/otel"
)

func (s *Server) RegisterHandlersComAtproto(e *echo.Echo) error {
	e.POST("/xrpc/com.atproto.admin.disableAccountInvites", s.HandleComAtprotoAdminDisableAccountInvites)
	e.POST("/xrpc/com.atproto.admin.disableInviteCodes", s.HandleComAtprotoAdminDisableInviteCodes)
	e.POST("/xrpc/com.atproto.admin.enableAccountInvites", s.HandleComAtprotoAdminEnableAccountInvites)
	e.GET("/xrpc/com.atproto.admin.getAccountInfo", s.HandleComAtprotoAdminGetAccountInfo)
	e.GET("/xrpc/com.atproto.admin.getInviteCodes", s.HandleComAtprotoAdminGetInviteCodes)
	e.GET("/xrpc/com.atproto.admin.getSubjectStatus", s.HandleComAtprotoAdminGetSubjectStatus)
	e.POST("/xrpc/com.atproto.admin.sendEmail", s.HandleComAtprotoAdminSendEmail)
	e.POST("/xrpc/com.atproto.admin.updateAccountEmail", s.HandleComAtprotoAdminUpdateAccountEmail)
	e.POST("/xrpc/com.atproto.admin.updateAccountHandle", s.HandleComAtprotoAdminUpdateAccountHandle)
	e.POST("/xrpc/com.atproto.admin.updateSubjectStatus", s.HandleComAtprotoAdminUpdateSubjectStatus)
	e.GET("/xrpc/com.atproto.identity.resolveHandle", s.HandleComAtprotoIdentityResolveHandle)
	e.POST("/xrpc/com.atproto.identity.updateHandle", s.HandleComAtprotoIdentityUpdateHandle)
	e.GET("/xrpc/com.atproto.label.queryLabels", s.HandleComAtprotoLabelQueryLabels)
	e.POST("/xrpc/com.atproto.moderation.createReport", s.HandleComAtprotoModerationCreateReport)
	e.POST("/xrpc/com.atproto.repo.applyWrites", s.HandleComAtprotoRepoApplyWrites)
	e.POST("/xrpc/com.atproto.repo.createRecord", s.HandleComAtprotoRepoCreateRecord)
	e.POST("/xrpc/com.atproto.repo.deleteRecord", s.HandleComAtprotoRepoDeleteRecord)
	e.GET("/xrpc/com.atproto.repo.describeRepo", s.HandleComAtprotoRepoDescribeRepo)
	e.GET("/xrpc/com.atproto.repo.getRecord", s.HandleComAtprotoRepoGetRecord)
	e.GET("/xrpc/com.atproto.repo.listRecords", s.HandleComAtprotoRepoListRecords)
	e.POST("/xrpc/com.atproto.repo.putRecord", s.HandleComAtprotoRepoPutRecord)
	e.POST("/xrpc/com.atproto.repo.uploadBlob", s.HandleComAtprotoRepoUploadBlob)
	e.POST("/xrpc/com.atproto.server.confirmEmail", s.HandleComAtprotoServerConfirmEmail)
	e.POST("/xrpc/com.atproto.server.createAccount", s.HandleComAtprotoServerCreateAccount)
	e.POST("/xrpc/com.atproto.server.createAppPassword", s.HandleComAtprotoServerCreateAppPassword)
	e.POST("/xrpc/com.atproto.server.createInviteCode", s.HandleComAtprotoServerCreateInviteCode)
	e.POST("/xrpc/com.atproto.server.createInviteCodes", s.HandleComAtprotoServerCreateInviteCodes)
	e.POST("/xrpc/com.atproto.server.createSession", s.HandleComAtprotoServerCreateSession)
	e.POST("/xrpc/com.atproto.server.deleteAccount", s.HandleComAtprotoServerDeleteAccount)
	e.POST("/xrpc/com.atproto.server.deleteSession", s.HandleComAtprotoServerDeleteSession)
	e.GET("/xrpc/com.atproto.server.describeServer", s.HandleComAtprotoServerDescribeServer)
	e.GET("/xrpc/com.atproto.server.getAccountInviteCodes", s.HandleComAtprotoServerGetAccountInviteCodes)
	e.GET("/xrpc/com.atproto.server.getSession", s.HandleComAtprotoServerGetSession)
	e.GET("/xrpc/com.atproto.server.listAppPasswords", s.HandleComAtprotoServerListAppPasswords)
	e.POST("/xrpc/com.atproto.server.refreshSession", s.HandleComAtprotoServerRefreshSession)
	e.POST("/xrpc/com.atproto.server.requestAccountDelete", s.HandleComAtprotoServerRequestAccountDelete)
	e.POST("/xrpc/com.atproto.server.requestEmailConfirmation", s.HandleComAtprotoServerRequestEmailConfirmation)
	e.POST("/xrpc/com.atproto.server.requestEmailUpdate", s.HandleComAtprotoServerRequestEmailUpdate)
	e.POST("/xrpc/com.atproto.server.requestPasswordReset", s.HandleComAtprotoServerRequestPasswordReset)
	e.POST("/xrpc/com.atproto.server.reserveSigningKey", s.HandleComAtprotoServerReserveSigningKey)
	e.POST("/xrpc/com.atproto.server.resetPassword", s.HandleComAtprotoServerResetPassword)
	e.POST("/xrpc/com.atproto.server.revokeAppPassword", s.HandleComAtprotoServerRevokeAppPassword)
	e.POST("/xrpc/com.atproto.server.updateEmail", s.HandleComAtprotoServerUpdateEmail)
	e.GET("/xrpc/com.atproto.sync.getBlob", s.HandleComAtprotoSyncGetBlob)
	e.GET("/xrpc/com.atproto.sync.getBlocks", s.HandleComAtprotoSyncGetBlocks)
	e.GET("/xrpc/com.atproto.sync.getCheckout", s.HandleComAtprotoSyncGetCheckout)
	e.GET("/xrpc/com.atproto.sync.getHead", s.HandleComAtprotoSyncGetHead)
	e.GET("/xrpc/com.atproto.sync.getLatestCommit", s.HandleComAtprotoSyncGetLatestCommit)
	e.GET("/xrpc/com.atproto.sync.getRecord", s.HandleComAtprotoSyncGetRecord)
	e.GET("/xrpc/com.atproto.sync.getRepo", s.HandleComAtprotoSyncGetRepo)
	e.GET("/xrpc/com.atproto.sync.listBlobs", s.HandleComAtprotoSyncListBlobs)
	e.GET("/xrpc/com.atproto.sync.listRepos", s.HandleComAtprotoSyncListRepos)
	e.POST("/xrpc/com.atproto.sync.notifyOfUpdate", s.HandleComAtprotoSyncNotifyOfUpdate)
	e.POST("/xrpc/com.atproto.sync.requestCrawl", s.HandleComAtprotoSyncRequestCrawl)
	e.GET("/xrpc/com.atproto.temp.fetchLabels", s.HandleComAtprotoTempFetchLabels)
	return nil
}

func (s *Server) HandleComAtprotoAdminDisableAccountInvites(c echo.Context) error {
	ctx, span := otel.Tracer("server").Start(c.Request().Context(), "HandleComAtprotoAdminDisableAccountInvites")
	defer span.End()

	var body comatprototypes.AdminDisableAccountInvites_Input
	if err := c.Bind(&body); err != nil {
		return err
	}
	var handleErr error
	// func (s *Server) handleComAtprotoAdminDisableAccountInvites(ctx context.Context,body *comatprototypes.AdminDisableAccountInvites_Input) error
	handleErr = s.handleComAtprotoAdminDisableAccountInvites(ctx, &body)
	if handleErr != nil {
		return handleErr
	}
	return nil
}

func (s *Server) HandleComAtprotoAdminDisableInviteCodes(c echo.Context) error {
	ctx, span := otel.Tracer("server").Start(c.Request().Context(), "HandleComAtprotoAdminDisableInviteCodes")
	defer span.End()

	var body comatprototypes.AdminDisableInviteCodes_Input
	if err := c.Bind(&body); err != nil {
		return err
	}
	var handleErr error
	// func (s *Server) handleComAtprotoAdminDisableInviteCodes(ctx context.Context,body *comatprototypes.AdminDisableInviteCodes_Input) error
	handleErr = s.handleComAtprotoAdminDisableInviteCodes(ctx, &body)
	if handleErr != nil {
		return handleErr
	}
	return nil
}

func (s *Server) HandleComAtprotoAdminEnableAccountInvites(c echo.Context) error {
	ctx, span := otel.Tracer("server").Start(c.Request().Context(), "HandleComAtprotoAdminEnableAccountInvites")
	defer span.End()

	var body comatprototypes.AdminEnableAccountInvites_Input
	if err := c.Bind(&body); err != nil {
		return err
	}
	var handleErr error
	// func (s *Server) handleComAtprotoAdminEnableAccountInvites(ctx context.Context,body *comatprototypes.AdminEnableAccountInvites_Input) error
	handleErr = s.handleComAtprotoAdminEnableAccountInvites(ctx, &body)
	if handleErr != nil {
		return handleErr
	}
	return nil
}

func (s *Server) HandleComAtprotoAdminGetAccountInfo(c echo.Context) error {
	ctx, span := otel.Tracer("server").Start(c.Request().Context(), "HandleComAtprotoAdminGetAccountInfo")
	defer span.End()
	did := c.QueryParam("did")
	var out *comatprototypes.AdminDefs_AccountView
	var handleErr error
	// func (s *Server) handleComAtprotoAdminGetAccountInfo(ctx context.Context,did string) (*comatprototypes.AdminDefs_AccountView, error)
	out, handleErr = s.handleComAtprotoAdminGetAccountInfo(ctx, did)
	if handleErr != nil {
		return handleErr
	}
	return c.JSON(200, out)
}

func (s *Server) HandleComAtprotoAdminGetInviteCodes(c echo.Context) error {
	ctx, span := otel.Tracer("server").Start(c.Request().Context(), "HandleComAtprotoAdminGetInviteCodes")
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
		limit = 100
	}
	sort := c.QueryParam("sort")
	var out *comatprototypes.AdminGetInviteCodes_Output
	var handleErr error
	// func (s *Server) handleComAtprotoAdminGetInviteCodes(ctx context.Context,cursor string,limit int,sort string) (*comatprototypes.AdminGetInviteCodes_Output, error)
	out, handleErr = s.handleComAtprotoAdminGetInviteCodes(ctx, cursor, limit, sort)
	if handleErr != nil {
		return handleErr
	}
	return c.JSON(200, out)
}

func (s *Server) HandleComAtprotoAdminGetSubjectStatus(c echo.Context) error {
	ctx, span := otel.Tracer("server").Start(c.Request().Context(), "HandleComAtprotoAdminGetSubjectStatus")
	defer span.End()
	blob := c.QueryParam("blob")
	did := c.QueryParam("did")
	uri := c.QueryParam("uri")
	var out *comatprototypes.AdminGetSubjectStatus_Output
	var handleErr error
	// func (s *Server) handleComAtprotoAdminGetSubjectStatus(ctx context.Context,blob string,did string,uri string) (*comatprototypes.AdminGetSubjectStatus_Output, error)
	out, handleErr = s.handleComAtprotoAdminGetSubjectStatus(ctx, blob, did, uri)
	if handleErr != nil {
		return handleErr
	}
	return c.JSON(200, out)
}

func (s *Server) HandleComAtprotoAdminSendEmail(c echo.Context) error {
	ctx, span := otel.Tracer("server").Start(c.Request().Context(), "HandleComAtprotoAdminSendEmail")
	defer span.End()

	var body comatprototypes.AdminSendEmail_Input
	if err := c.Bind(&body); err != nil {
		return err
	}
	var out *comatprototypes.AdminSendEmail_Output
	var handleErr error
	// func (s *Server) handleComAtprotoAdminSendEmail(ctx context.Context,body *comatprototypes.AdminSendEmail_Input) (*comatprototypes.AdminSendEmail_Output, error)
	out, handleErr = s.handleComAtprotoAdminSendEmail(ctx, &body)
	if handleErr != nil {
		return handleErr
	}
	return c.JSON(200, out)
}

func (s *Server) HandleComAtprotoAdminUpdateAccountEmail(c echo.Context) error {
	ctx, span := otel.Tracer("server").Start(c.Request().Context(), "HandleComAtprotoAdminUpdateAccountEmail")
	defer span.End()

	var body comatprototypes.AdminUpdateAccountEmail_Input
	if err := c.Bind(&body); err != nil {
		return err
	}
	var handleErr error
	// func (s *Server) handleComAtprotoAdminUpdateAccountEmail(ctx context.Context,body *comatprototypes.AdminUpdateAccountEmail_Input) error
	handleErr = s.handleComAtprotoAdminUpdateAccountEmail(ctx, &body)
	if handleErr != nil {
		return handleErr
	}
	return nil
}

func (s *Server) HandleComAtprotoAdminUpdateAccountHandle(c echo.Context) error {
	ctx, span := otel.Tracer("server").Start(c.Request().Context(), "HandleComAtprotoAdminUpdateAccountHandle")
	defer span.End()

	var body comatprototypes.AdminUpdateAccountHandle_Input
	if err := c.Bind(&body); err != nil {
		return err
	}
	var handleErr error
	// func (s *Server) handleComAtprotoAdminUpdateAccountHandle(ctx context.Context,body *comatprototypes.AdminUpdateAccountHandle_Input) error
	handleErr = s.handleComAtprotoAdminUpdateAccountHandle(ctx, &body)
	if handleErr != nil {
		return handleErr
	}
	return nil
}

func (s *Server) HandleComAtprotoAdminUpdateSubjectStatus(c echo.Context) error {
	ctx, span := otel.Tracer("server").Start(c.Request().Context(), "HandleComAtprotoAdminUpdateSubjectStatus")
	defer span.End()

	var body comatprototypes.AdminUpdateSubjectStatus_Input
	if err := c.Bind(&body); err != nil {
		return err
	}
	var out *comatprototypes.AdminUpdateSubjectStatus_Output
	var handleErr error
	// func (s *Server) handleComAtprotoAdminUpdateSubjectStatus(ctx context.Context,body *comatprototypes.AdminUpdateSubjectStatus_Input) (*comatprototypes.AdminUpdateSubjectStatus_Output, error)
	out, handleErr = s.handleComAtprotoAdminUpdateSubjectStatus(ctx, &body)
	if handleErr != nil {
		return handleErr
	}
	return c.JSON(200, out)
}

func (s *Server) HandleComAtprotoIdentityResolveHandle(c echo.Context) error {
	ctx, span := otel.Tracer("server").Start(c.Request().Context(), "HandleComAtprotoIdentityResolveHandle")
	defer span.End()
	handle := c.QueryParam("handle")
	var out *comatprototypes.IdentityResolveHandle_Output
	var handleErr error
	// func (s *Server) handleComAtprotoIdentityResolveHandle(ctx context.Context,handle string) (*comatprototypes.IdentityResolveHandle_Output, error)
	out, handleErr = s.handleComAtprotoIdentityResolveHandle(ctx, handle)
	if handleErr != nil {
		return handleErr
	}
	return c.JSON(200, out)
}

func (s *Server) HandleComAtprotoIdentityUpdateHandle(c echo.Context) error {
	ctx, span := otel.Tracer("server").Start(c.Request().Context(), "HandleComAtprotoIdentityUpdateHandle")
	defer span.End()

	var body comatprototypes.IdentityUpdateHandle_Input
	if err := c.Bind(&body); err != nil {
		return err
	}
	var handleErr error
	// func (s *Server) handleComAtprotoIdentityUpdateHandle(ctx context.Context,body *comatprototypes.IdentityUpdateHandle_Input) error
	handleErr = s.handleComAtprotoIdentityUpdateHandle(ctx, &body)
	if handleErr != nil {
		return handleErr
	}
	return nil
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
		limit = 50
	}

	sources := c.QueryParams()["sources"]

	uriPatterns := c.QueryParams()["uriPatterns"]
	var out *comatprototypes.LabelQueryLabels_Output
	var handleErr error
	// func (s *Server) handleComAtprotoLabelQueryLabels(ctx context.Context,cursor string,limit int,sources []string,uriPatterns []string) (*comatprototypes.LabelQueryLabels_Output, error)
	out, handleErr = s.handleComAtprotoLabelQueryLabels(ctx, cursor, limit, sources, uriPatterns)
	if handleErr != nil {
		return handleErr
	}
	return c.JSON(200, out)
}

func (s *Server) HandleComAtprotoModerationCreateReport(c echo.Context) error {
	ctx, span := otel.Tracer("server").Start(c.Request().Context(), "HandleComAtprotoModerationCreateReport")
	defer span.End()

	var body comatprototypes.ModerationCreateReport_Input
	if err := c.Bind(&body); err != nil {
		return err
	}
	var out *comatprototypes.ModerationCreateReport_Output
	var handleErr error
	// func (s *Server) handleComAtprotoModerationCreateReport(ctx context.Context,body *comatprototypes.ModerationCreateReport_Input) (*comatprototypes.ModerationCreateReport_Output, error)
	out, handleErr = s.handleComAtprotoModerationCreateReport(ctx, &body)
	if handleErr != nil {
		return handleErr
	}
	return c.JSON(200, out)
}

func (s *Server) HandleComAtprotoRepoApplyWrites(c echo.Context) error {
	ctx, span := otel.Tracer("server").Start(c.Request().Context(), "HandleComAtprotoRepoApplyWrites")
	defer span.End()

	var body comatprototypes.RepoApplyWrites_Input
	if err := c.Bind(&body); err != nil {
		return err
	}
	var handleErr error
	// func (s *Server) handleComAtprotoRepoApplyWrites(ctx context.Context,body *comatprototypes.RepoApplyWrites_Input) error
	handleErr = s.handleComAtprotoRepoApplyWrites(ctx, &body)
	if handleErr != nil {
		return handleErr
	}
	return nil
}

func (s *Server) HandleComAtprotoRepoCreateRecord(c echo.Context) error {
	ctx, span := otel.Tracer("server").Start(c.Request().Context(), "HandleComAtprotoRepoCreateRecord")
	defer span.End()

	var body comatprototypes.RepoCreateRecord_Input
	if err := c.Bind(&body); err != nil {
		return err
	}
	var out *comatprototypes.RepoCreateRecord_Output
	var handleErr error
	// func (s *Server) handleComAtprotoRepoCreateRecord(ctx context.Context,body *comatprototypes.RepoCreateRecord_Input) (*comatprototypes.RepoCreateRecord_Output, error)
	out, handleErr = s.handleComAtprotoRepoCreateRecord(ctx, &body)
	if handleErr != nil {
		return handleErr
	}
	return c.JSON(200, out)
}

func (s *Server) HandleComAtprotoRepoDeleteRecord(c echo.Context) error {
	ctx, span := otel.Tracer("server").Start(c.Request().Context(), "HandleComAtprotoRepoDeleteRecord")
	defer span.End()

	var body comatprototypes.RepoDeleteRecord_Input
	if err := c.Bind(&body); err != nil {
		return err
	}
	var handleErr error
	// func (s *Server) handleComAtprotoRepoDeleteRecord(ctx context.Context,body *comatprototypes.RepoDeleteRecord_Input) error
	handleErr = s.handleComAtprotoRepoDeleteRecord(ctx, &body)
	if handleErr != nil {
		return handleErr
	}
	return nil
}

func (s *Server) HandleComAtprotoRepoDescribeRepo(c echo.Context) error {
	ctx, span := otel.Tracer("server").Start(c.Request().Context(), "HandleComAtprotoRepoDescribeRepo")
	defer span.End()
	repo := c.QueryParam("repo")
	var out *comatprototypes.RepoDescribeRepo_Output
	var handleErr error
	// func (s *Server) handleComAtprotoRepoDescribeRepo(ctx context.Context,repo string) (*comatprototypes.RepoDescribeRepo_Output, error)
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
	var out *comatprototypes.RepoGetRecord_Output
	var handleErr error
	// func (s *Server) handleComAtprotoRepoGetRecord(ctx context.Context,cid string,collection string,repo string,rkey string) (*comatprototypes.RepoGetRecord_Output, error)
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
	cursor := c.QueryParam("cursor")

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
	var out *comatprototypes.RepoListRecords_Output
	var handleErr error
	// func (s *Server) handleComAtprotoRepoListRecords(ctx context.Context,collection string,cursor string,limit int,repo string,reverse *bool,rkeyEnd string,rkeyStart string) (*comatprototypes.RepoListRecords_Output, error)
	out, handleErr = s.handleComAtprotoRepoListRecords(ctx, collection, cursor, limit, repo, reverse, rkeyEnd, rkeyStart)
	if handleErr != nil {
		return handleErr
	}
	return c.JSON(200, out)
}

func (s *Server) HandleComAtprotoRepoPutRecord(c echo.Context) error {
	ctx, span := otel.Tracer("server").Start(c.Request().Context(), "HandleComAtprotoRepoPutRecord")
	defer span.End()

	var body comatprototypes.RepoPutRecord_Input
	if err := c.Bind(&body); err != nil {
		return err
	}
	var out *comatprototypes.RepoPutRecord_Output
	var handleErr error
	// func (s *Server) handleComAtprotoRepoPutRecord(ctx context.Context,body *comatprototypes.RepoPutRecord_Input) (*comatprototypes.RepoPutRecord_Output, error)
	out, handleErr = s.handleComAtprotoRepoPutRecord(ctx, &body)
	if handleErr != nil {
		return handleErr
	}
	return c.JSON(200, out)
}

func (s *Server) HandleComAtprotoRepoUploadBlob(c echo.Context) error {
	ctx, span := otel.Tracer("server").Start(c.Request().Context(), "HandleComAtprotoRepoUploadBlob")
	defer span.End()
	body := c.Request().Body
	contentType := c.Request().Header.Get("Content-Type")
	var out *comatprototypes.RepoUploadBlob_Output
	var handleErr error
	// func (s *Server) handleComAtprotoRepoUploadBlob(ctx context.Context,r io.Reader,contentType string) (*comatprototypes.RepoUploadBlob_Output, error)
	out, handleErr = s.handleComAtprotoRepoUploadBlob(ctx, body, contentType)
	if handleErr != nil {
		return handleErr
	}
	return c.JSON(200, out)
}

func (s *Server) HandleComAtprotoServerConfirmEmail(c echo.Context) error {
	ctx, span := otel.Tracer("server").Start(c.Request().Context(), "HandleComAtprotoServerConfirmEmail")
	defer span.End()

	var body comatprototypes.ServerConfirmEmail_Input
	if err := c.Bind(&body); err != nil {
		return err
	}
	var handleErr error
	// func (s *Server) handleComAtprotoServerConfirmEmail(ctx context.Context,body *comatprototypes.ServerConfirmEmail_Input) error
	handleErr = s.handleComAtprotoServerConfirmEmail(ctx, &body)
	if handleErr != nil {
		return handleErr
	}
	return nil
}

func (s *Server) HandleComAtprotoServerCreateAccount(c echo.Context) error {
	ctx, span := otel.Tracer("server").Start(c.Request().Context(), "HandleComAtprotoServerCreateAccount")
	defer span.End()

	var body comatprototypes.ServerCreateAccount_Input
	if err := c.Bind(&body); err != nil {
		return err
	}
	var out *comatprototypes.ServerCreateAccount_Output
	var handleErr error
	// func (s *Server) handleComAtprotoServerCreateAccount(ctx context.Context,body *comatprototypes.ServerCreateAccount_Input) (*comatprototypes.ServerCreateAccount_Output, error)
	out, handleErr = s.handleComAtprotoServerCreateAccount(ctx, &body)
	if handleErr != nil {
		return handleErr
	}
	return c.JSON(200, out)
}

func (s *Server) HandleComAtprotoServerCreateAppPassword(c echo.Context) error {
	ctx, span := otel.Tracer("server").Start(c.Request().Context(), "HandleComAtprotoServerCreateAppPassword")
	defer span.End()

	var body comatprototypes.ServerCreateAppPassword_Input
	if err := c.Bind(&body); err != nil {
		return err
	}
	var out *comatprototypes.ServerCreateAppPassword_AppPassword
	var handleErr error
	// func (s *Server) handleComAtprotoServerCreateAppPassword(ctx context.Context,body *comatprototypes.ServerCreateAppPassword_Input) (*comatprototypes.ServerCreateAppPassword_AppPassword, error)
	out, handleErr = s.handleComAtprotoServerCreateAppPassword(ctx, &body)
	if handleErr != nil {
		return handleErr
	}
	return c.JSON(200, out)
}

func (s *Server) HandleComAtprotoServerCreateInviteCode(c echo.Context) error {
	ctx, span := otel.Tracer("server").Start(c.Request().Context(), "HandleComAtprotoServerCreateInviteCode")
	defer span.End()

	var body comatprototypes.ServerCreateInviteCode_Input
	if err := c.Bind(&body); err != nil {
		return err
	}
	var out *comatprototypes.ServerCreateInviteCode_Output
	var handleErr error
	// func (s *Server) handleComAtprotoServerCreateInviteCode(ctx context.Context,body *comatprototypes.ServerCreateInviteCode_Input) (*comatprototypes.ServerCreateInviteCode_Output, error)
	out, handleErr = s.handleComAtprotoServerCreateInviteCode(ctx, &body)
	if handleErr != nil {
		return handleErr
	}
	return c.JSON(200, out)
}

func (s *Server) HandleComAtprotoServerCreateInviteCodes(c echo.Context) error {
	ctx, span := otel.Tracer("server").Start(c.Request().Context(), "HandleComAtprotoServerCreateInviteCodes")
	defer span.End()

	var body comatprototypes.ServerCreateInviteCodes_Input
	if err := c.Bind(&body); err != nil {
		return err
	}
	var out *comatprototypes.ServerCreateInviteCodes_Output
	var handleErr error
	// func (s *Server) handleComAtprotoServerCreateInviteCodes(ctx context.Context,body *comatprototypes.ServerCreateInviteCodes_Input) (*comatprototypes.ServerCreateInviteCodes_Output, error)
	out, handleErr = s.handleComAtprotoServerCreateInviteCodes(ctx, &body)
	if handleErr != nil {
		return handleErr
	}
	return c.JSON(200, out)
}

func (s *Server) HandleComAtprotoServerCreateSession(c echo.Context) error {
	ctx, span := otel.Tracer("server").Start(c.Request().Context(), "HandleComAtprotoServerCreateSession")
	defer span.End()

	var body comatprototypes.ServerCreateSession_Input
	if err := c.Bind(&body); err != nil {
		return err
	}
	var out *comatprototypes.ServerCreateSession_Output
	var handleErr error
	// func (s *Server) handleComAtprotoServerCreateSession(ctx context.Context,body *comatprototypes.ServerCreateSession_Input) (*comatprototypes.ServerCreateSession_Output, error)
	out, handleErr = s.handleComAtprotoServerCreateSession(ctx, &body)
	if handleErr != nil {
		return handleErr
	}
	return c.JSON(200, out)
}

func (s *Server) HandleComAtprotoServerDeleteAccount(c echo.Context) error {
	ctx, span := otel.Tracer("server").Start(c.Request().Context(), "HandleComAtprotoServerDeleteAccount")
	defer span.End()

	var body comatprototypes.ServerDeleteAccount_Input
	if err := c.Bind(&body); err != nil {
		return err
	}
	var handleErr error
	// func (s *Server) handleComAtprotoServerDeleteAccount(ctx context.Context,body *comatprototypes.ServerDeleteAccount_Input) error
	handleErr = s.handleComAtprotoServerDeleteAccount(ctx, &body)
	if handleErr != nil {
		return handleErr
	}
	return nil
}

func (s *Server) HandleComAtprotoServerDeleteSession(c echo.Context) error {
	ctx, span := otel.Tracer("server").Start(c.Request().Context(), "HandleComAtprotoServerDeleteSession")
	defer span.End()
	var handleErr error
	// func (s *Server) handleComAtprotoServerDeleteSession(ctx context.Context) error
	handleErr = s.handleComAtprotoServerDeleteSession(ctx)
	if handleErr != nil {
		return handleErr
	}
	return nil
}

func (s *Server) HandleComAtprotoServerDescribeServer(c echo.Context) error {
	ctx, span := otel.Tracer("server").Start(c.Request().Context(), "HandleComAtprotoServerDescribeServer")
	defer span.End()
	var out *comatprototypes.ServerDescribeServer_Output
	var handleErr error
	// func (s *Server) handleComAtprotoServerDescribeServer(ctx context.Context) (*comatprototypes.ServerDescribeServer_Output, error)
	out, handleErr = s.handleComAtprotoServerDescribeServer(ctx)
	if handleErr != nil {
		return handleErr
	}
	return c.JSON(200, out)
}

func (s *Server) HandleComAtprotoServerGetAccountInviteCodes(c echo.Context) error {
	ctx, span := otel.Tracer("server").Start(c.Request().Context(), "HandleComAtprotoServerGetAccountInviteCodes")
	defer span.End()

	var createAvailable bool
	if p := c.QueryParam("createAvailable"); p != "" {
		var err error
		createAvailable, err = strconv.ParseBool(p)
		if err != nil {
			return err
		}
	} else {
		createAvailable = true
	}

	var includeUsed bool
	if p := c.QueryParam("includeUsed"); p != "" {
		var err error
		includeUsed, err = strconv.ParseBool(p)
		if err != nil {
			return err
		}
	} else {
		includeUsed = true
	}
	var out *comatprototypes.ServerGetAccountInviteCodes_Output
	var handleErr error
	// func (s *Server) handleComAtprotoServerGetAccountInviteCodes(ctx context.Context,createAvailable bool,includeUsed bool) (*comatprototypes.ServerGetAccountInviteCodes_Output, error)
	out, handleErr = s.handleComAtprotoServerGetAccountInviteCodes(ctx, createAvailable, includeUsed)
	if handleErr != nil {
		return handleErr
	}
	return c.JSON(200, out)
}

func (s *Server) HandleComAtprotoServerGetSession(c echo.Context) error {
	ctx, span := otel.Tracer("server").Start(c.Request().Context(), "HandleComAtprotoServerGetSession")
	defer span.End()
	var out *comatprototypes.ServerGetSession_Output
	var handleErr error
	// func (s *Server) handleComAtprotoServerGetSession(ctx context.Context) (*comatprototypes.ServerGetSession_Output, error)
	out, handleErr = s.handleComAtprotoServerGetSession(ctx)
	if handleErr != nil {
		return handleErr
	}
	return c.JSON(200, out)
}

func (s *Server) HandleComAtprotoServerListAppPasswords(c echo.Context) error {
	ctx, span := otel.Tracer("server").Start(c.Request().Context(), "HandleComAtprotoServerListAppPasswords")
	defer span.End()
	var out *comatprototypes.ServerListAppPasswords_Output
	var handleErr error
	// func (s *Server) handleComAtprotoServerListAppPasswords(ctx context.Context) (*comatprototypes.ServerListAppPasswords_Output, error)
	out, handleErr = s.handleComAtprotoServerListAppPasswords(ctx)
	if handleErr != nil {
		return handleErr
	}
	return c.JSON(200, out)
}

func (s *Server) HandleComAtprotoServerRefreshSession(c echo.Context) error {
	ctx, span := otel.Tracer("server").Start(c.Request().Context(), "HandleComAtprotoServerRefreshSession")
	defer span.End()
	var out *comatprototypes.ServerRefreshSession_Output
	var handleErr error
	// func (s *Server) handleComAtprotoServerRefreshSession(ctx context.Context) (*comatprototypes.ServerRefreshSession_Output, error)
	out, handleErr = s.handleComAtprotoServerRefreshSession(ctx)
	if handleErr != nil {
		return handleErr
	}
	return c.JSON(200, out)
}

func (s *Server) HandleComAtprotoServerRequestAccountDelete(c echo.Context) error {
	ctx, span := otel.Tracer("server").Start(c.Request().Context(), "HandleComAtprotoServerRequestAccountDelete")
	defer span.End()
	var handleErr error
	// func (s *Server) handleComAtprotoServerRequestAccountDelete(ctx context.Context) error
	handleErr = s.handleComAtprotoServerRequestAccountDelete(ctx)
	if handleErr != nil {
		return handleErr
	}
	return nil
}

func (s *Server) HandleComAtprotoServerRequestEmailConfirmation(c echo.Context) error {
	ctx, span := otel.Tracer("server").Start(c.Request().Context(), "HandleComAtprotoServerRequestEmailConfirmation")
	defer span.End()
	var handleErr error
	// func (s *Server) handleComAtprotoServerRequestEmailConfirmation(ctx context.Context) error
	handleErr = s.handleComAtprotoServerRequestEmailConfirmation(ctx)
	if handleErr != nil {
		return handleErr
	}
	return nil
}

func (s *Server) HandleComAtprotoServerRequestEmailUpdate(c echo.Context) error {
	ctx, span := otel.Tracer("server").Start(c.Request().Context(), "HandleComAtprotoServerRequestEmailUpdate")
	defer span.End()
	var out *comatprototypes.ServerRequestEmailUpdate_Output
	var handleErr error
	// func (s *Server) handleComAtprotoServerRequestEmailUpdate(ctx context.Context) (*comatprototypes.ServerRequestEmailUpdate_Output, error)
	out, handleErr = s.handleComAtprotoServerRequestEmailUpdate(ctx)
	if handleErr != nil {
		return handleErr
	}
	return c.JSON(200, out)
}

func (s *Server) HandleComAtprotoServerRequestPasswordReset(c echo.Context) error {
	ctx, span := otel.Tracer("server").Start(c.Request().Context(), "HandleComAtprotoServerRequestPasswordReset")
	defer span.End()

	var body comatprototypes.ServerRequestPasswordReset_Input
	if err := c.Bind(&body); err != nil {
		return err
	}
	var handleErr error
	// func (s *Server) handleComAtprotoServerRequestPasswordReset(ctx context.Context,body *comatprototypes.ServerRequestPasswordReset_Input) error
	handleErr = s.handleComAtprotoServerRequestPasswordReset(ctx, &body)
	if handleErr != nil {
		return handleErr
	}
	return nil
}

func (s *Server) HandleComAtprotoServerReserveSigningKey(c echo.Context) error {
	ctx, span := otel.Tracer("server").Start(c.Request().Context(), "HandleComAtprotoServerReserveSigningKey")
	defer span.End()

	var body comatprototypes.ServerReserveSigningKey_Input
	if err := c.Bind(&body); err != nil {
		return err
	}
	var out *comatprototypes.ServerReserveSigningKey_Output
	var handleErr error
	// func (s *Server) handleComAtprotoServerReserveSigningKey(ctx context.Context,body *comatprototypes.ServerReserveSigningKey_Input) (*comatprototypes.ServerReserveSigningKey_Output, error)
	out, handleErr = s.handleComAtprotoServerReserveSigningKey(ctx, &body)
	if handleErr != nil {
		return handleErr
	}
	return c.JSON(200, out)
}

func (s *Server) HandleComAtprotoServerResetPassword(c echo.Context) error {
	ctx, span := otel.Tracer("server").Start(c.Request().Context(), "HandleComAtprotoServerResetPassword")
	defer span.End()

	var body comatprototypes.ServerResetPassword_Input
	if err := c.Bind(&body); err != nil {
		return err
	}
	var handleErr error
	// func (s *Server) handleComAtprotoServerResetPassword(ctx context.Context,body *comatprototypes.ServerResetPassword_Input) error
	handleErr = s.handleComAtprotoServerResetPassword(ctx, &body)
	if handleErr != nil {
		return handleErr
	}
	return nil
}

func (s *Server) HandleComAtprotoServerRevokeAppPassword(c echo.Context) error {
	ctx, span := otel.Tracer("server").Start(c.Request().Context(), "HandleComAtprotoServerRevokeAppPassword")
	defer span.End()

	var body comatprototypes.ServerRevokeAppPassword_Input
	if err := c.Bind(&body); err != nil {
		return err
	}
	var handleErr error
	// func (s *Server) handleComAtprotoServerRevokeAppPassword(ctx context.Context,body *comatprototypes.ServerRevokeAppPassword_Input) error
	handleErr = s.handleComAtprotoServerRevokeAppPassword(ctx, &body)
	if handleErr != nil {
		return handleErr
	}
	return nil
}

func (s *Server) HandleComAtprotoServerUpdateEmail(c echo.Context) error {
	ctx, span := otel.Tracer("server").Start(c.Request().Context(), "HandleComAtprotoServerUpdateEmail")
	defer span.End()

	var body comatprototypes.ServerUpdateEmail_Input
	if err := c.Bind(&body); err != nil {
		return err
	}
	var handleErr error
	// func (s *Server) handleComAtprotoServerUpdateEmail(ctx context.Context,body *comatprototypes.ServerUpdateEmail_Input) error
	handleErr = s.handleComAtprotoServerUpdateEmail(ctx, &body)
	if handleErr != nil {
		return handleErr
	}
	return nil
}

func (s *Server) HandleComAtprotoSyncGetBlob(c echo.Context) error {
	ctx, span := otel.Tracer("server").Start(c.Request().Context(), "HandleComAtprotoSyncGetBlob")
	defer span.End()
	cid := c.QueryParam("cid")
	did := c.QueryParam("did")
	var out io.Reader
	var handleErr error
	// func (s *Server) handleComAtprotoSyncGetBlob(ctx context.Context,cid string,did string) (io.Reader, error)
	out, handleErr = s.handleComAtprotoSyncGetBlob(ctx, cid, did)
	if handleErr != nil {
		return handleErr
	}
	return c.Stream(200, "application/octet-stream", out)
}

func (s *Server) HandleComAtprotoSyncGetBlocks(c echo.Context) error {
	ctx, span := otel.Tracer("server").Start(c.Request().Context(), "HandleComAtprotoSyncGetBlocks")
	defer span.End()

	cids := c.QueryParams()["cids"]
	did := c.QueryParam("did")
	var out io.Reader
	var handleErr error
	// func (s *Server) handleComAtprotoSyncGetBlocks(ctx context.Context,cids []string,did string) (io.Reader, error)
	out, handleErr = s.handleComAtprotoSyncGetBlocks(ctx, cids, did)
	if handleErr != nil {
		return handleErr
	}
	return c.Stream(200, "application/vnd.ipld.car", out)
}

func (s *Server) HandleComAtprotoSyncGetCheckout(c echo.Context) error {
	ctx, span := otel.Tracer("server").Start(c.Request().Context(), "HandleComAtprotoSyncGetCheckout")
	defer span.End()
	did := c.QueryParam("did")
	var out io.Reader
	var handleErr error
	// func (s *Server) handleComAtprotoSyncGetCheckout(ctx context.Context,did string) (io.Reader, error)
	out, handleErr = s.handleComAtprotoSyncGetCheckout(ctx, did)
	if handleErr != nil {
		return handleErr
	}
	return c.Stream(200, "application/vnd.ipld.car", out)
}

func (s *Server) HandleComAtprotoSyncGetHead(c echo.Context) error {
	ctx, span := otel.Tracer("server").Start(c.Request().Context(), "HandleComAtprotoSyncGetHead")
	defer span.End()
	did := c.QueryParam("did")
	var out *comatprototypes.SyncGetHead_Output
	var handleErr error
	// func (s *Server) handleComAtprotoSyncGetHead(ctx context.Context,did string) (*comatprototypes.SyncGetHead_Output, error)
	out, handleErr = s.handleComAtprotoSyncGetHead(ctx, did)
	if handleErr != nil {
		return handleErr
	}
	return c.JSON(200, out)
}

func (s *Server) HandleComAtprotoSyncGetLatestCommit(c echo.Context) error {
	ctx, span := otel.Tracer("server").Start(c.Request().Context(), "HandleComAtprotoSyncGetLatestCommit")
	defer span.End()
	did := c.QueryParam("did")
	var out *comatprototypes.SyncGetLatestCommit_Output
	var handleErr error
	// func (s *Server) handleComAtprotoSyncGetLatestCommit(ctx context.Context,did string) (*comatprototypes.SyncGetLatestCommit_Output, error)
	out, handleErr = s.handleComAtprotoSyncGetLatestCommit(ctx, did)
	if handleErr != nil {
		return handleErr
	}
	return c.JSON(200, out)
}

func (s *Server) HandleComAtprotoSyncGetRecord(c echo.Context) error {
	ctx, span := otel.Tracer("server").Start(c.Request().Context(), "HandleComAtprotoSyncGetRecord")
	defer span.End()
	collection := c.QueryParam("collection")
	commit := c.QueryParam("commit")
	did := c.QueryParam("did")
	rkey := c.QueryParam("rkey")
	var out io.Reader
	var handleErr error
	// func (s *Server) handleComAtprotoSyncGetRecord(ctx context.Context,collection string,commit string,did string,rkey string) (io.Reader, error)
	out, handleErr = s.handleComAtprotoSyncGetRecord(ctx, collection, commit, did, rkey)
	if handleErr != nil {
		return handleErr
	}
	return c.Stream(200, "application/vnd.ipld.car", out)
}

func (s *Server) HandleComAtprotoSyncGetRepo(c echo.Context) error {
	ctx, span := otel.Tracer("server").Start(c.Request().Context(), "HandleComAtprotoSyncGetRepo")
	defer span.End()
	did := c.QueryParam("did")
	since := c.QueryParam("since")
	var out io.Reader
	var handleErr error
	// func (s *Server) handleComAtprotoSyncGetRepo(ctx context.Context,did string,since string) (io.Reader, error)
	out, handleErr = s.handleComAtprotoSyncGetRepo(ctx, did, since)
	if handleErr != nil {
		return handleErr
	}
	return c.Stream(200, "application/vnd.ipld.car", out)
}

func (s *Server) HandleComAtprotoSyncListBlobs(c echo.Context) error {
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
	// func (s *Server) handleComAtprotoSyncListBlobs(ctx context.Context,cursor string,did string,limit int,since string) (*comatprototypes.SyncListBlobs_Output, error)
	out, handleErr = s.handleComAtprotoSyncListBlobs(ctx, cursor, did, limit, since)
	if handleErr != nil {
		return handleErr
	}
	return c.JSON(200, out)
}

func (s *Server) HandleComAtprotoSyncListRepos(c echo.Context) error {
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
	// func (s *Server) handleComAtprotoSyncListRepos(ctx context.Context,cursor string,limit int) (*comatprototypes.SyncListRepos_Output, error)
	out, handleErr = s.handleComAtprotoSyncListRepos(ctx, cursor, limit)
	if handleErr != nil {
		return handleErr
	}
	return c.JSON(200, out)
}

func (s *Server) HandleComAtprotoSyncNotifyOfUpdate(c echo.Context) error {
	ctx, span := otel.Tracer("server").Start(c.Request().Context(), "HandleComAtprotoSyncNotifyOfUpdate")
	defer span.End()

	var body comatprototypes.SyncNotifyOfUpdate_Input
	if err := c.Bind(&body); err != nil {
		return err
	}
	var handleErr error
	// func (s *Server) handleComAtprotoSyncNotifyOfUpdate(ctx context.Context,body *comatprototypes.SyncNotifyOfUpdate_Input) error
	handleErr = s.handleComAtprotoSyncNotifyOfUpdate(ctx, &body)
	if handleErr != nil {
		return handleErr
	}
	return nil
}

func (s *Server) HandleComAtprotoSyncRequestCrawl(c echo.Context) error {
	ctx, span := otel.Tracer("server").Start(c.Request().Context(), "HandleComAtprotoSyncRequestCrawl")
	defer span.End()

	var body comatprototypes.SyncRequestCrawl_Input
	if err := c.Bind(&body); err != nil {
		return err
	}
	var handleErr error
	// func (s *Server) handleComAtprotoSyncRequestCrawl(ctx context.Context,body *comatprototypes.SyncRequestCrawl_Input) error
	handleErr = s.handleComAtprotoSyncRequestCrawl(ctx, &body)
	if handleErr != nil {
		return handleErr
	}
	return nil
}

func (s *Server) HandleComAtprotoTempFetchLabels(c echo.Context) error {
	ctx, span := otel.Tracer("server").Start(c.Request().Context(), "HandleComAtprotoTempFetchLabels")
	defer span.End()

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

	var since *int
	if p := c.QueryParam("since"); p != "" {
		since_val, err := strconv.Atoi(p)
		if err != nil {
			return err
		}
		since = &since_val
	}
	var out *comatprototypes.TempFetchLabels_Output
	var handleErr error
	// func (s *Server) handleComAtprotoTempFetchLabels(ctx context.Context,limit int,since *int) (*comatprototypes.TempFetchLabels_Output, error)
	out, handleErr = s.handleComAtprotoTempFetchLabels(ctx, limit, since)
	if handleErr != nil {
		return handleErr
	}
	return c.JSON(200, out)
}
