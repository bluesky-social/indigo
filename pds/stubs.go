package pds

import (
	"io"
	"strconv"

	comatprototypes "github.com/bluesky-social/indigo/api/atproto"
	appbskytypes "github.com/bluesky-social/indigo/api/bsky"
	"github.com/labstack/echo/v4"
	"go.opentelemetry.io/otel"
)

func (s *Server) RegisterHandlersAppBsky(e *echo.Echo) error {
	e.GET("/xrpc/app.bsky.actor.getProfile", s.HandleAppBskyActorGetProfile)
	e.GET("/xrpc/app.bsky.actor.getProfiles", s.HandleAppBskyActorGetProfiles)
	e.GET("/xrpc/app.bsky.actor.getSuggestions", s.HandleAppBskyActorGetSuggestions)
	e.GET("/xrpc/app.bsky.actor.searchActors", s.HandleAppBskyActorSearchActors)
	e.GET("/xrpc/app.bsky.actor.searchActorsTypeahead", s.HandleAppBskyActorSearchActorsTypeahead)
	e.GET("/xrpc/app.bsky.feed.getAuthorFeed", s.HandleAppBskyFeedGetAuthorFeed)
	e.GET("/xrpc/app.bsky.feed.getLikes", s.HandleAppBskyFeedGetLikes)
	e.GET("/xrpc/app.bsky.feed.getPostThread", s.HandleAppBskyFeedGetPostThread)
	e.GET("/xrpc/app.bsky.feed.getRepostedBy", s.HandleAppBskyFeedGetRepostedBy)
	e.GET("/xrpc/app.bsky.feed.getTimeline", s.HandleAppBskyFeedGetTimeline)
	e.GET("/xrpc/app.bsky.graph.getFollowers", s.HandleAppBskyGraphGetFollowers)
	e.GET("/xrpc/app.bsky.graph.getFollows", s.HandleAppBskyGraphGetFollows)
	e.GET("/xrpc/app.bsky.graph.getMutes", s.HandleAppBskyGraphGetMutes)
	e.POST("/xrpc/app.bsky.graph.muteActor", s.HandleAppBskyGraphMuteActor)
	e.POST("/xrpc/app.bsky.graph.unmuteActor", s.HandleAppBskyGraphUnmuteActor)
	e.GET("/xrpc/app.bsky.notification.getUnreadCount", s.HandleAppBskyNotificationGetUnreadCount)
	e.GET("/xrpc/app.bsky.notification.listNotifications", s.HandleAppBskyNotificationListNotifications)
	e.POST("/xrpc/app.bsky.notification.updateSeen", s.HandleAppBskyNotificationUpdateSeen)
	e.GET("/xrpc/app.bsky.unspecced.getPopular", s.HandleAppBskyUnspeccedGetPopular)
	return nil
}

func (s *Server) HandleAppBskyActorGetProfile(c echo.Context) error {
	ctx, span := otel.Tracer("server").Start(c.Request().Context(), "HandleAppBskyActorGetProfile")
	defer span.End()
	actor := c.QueryParam("actor")
	var out *appbskytypes.ActorDefs_ProfileViewDetailed
	var handleErr error
	// func (s *Server) handleAppBskyActorGetProfile(ctx context.Context,actor string) (*appbskytypes.ActorDefs_ProfileViewDetailed, error)
	out, handleErr = s.handleAppBskyActorGetProfile(ctx, actor)
	if handleErr != nil {
		return handleErr
	}
	return c.JSON(200, out)
}

func (s *Server) HandleAppBskyActorGetProfiles(c echo.Context) error {
	ctx, span := otel.Tracer("server").Start(c.Request().Context(), "HandleAppBskyActorGetProfiles")
	defer span.End()

	actors := c.QueryParams()["actors"]
	var out *appbskytypes.ActorGetProfiles_Output
	var handleErr error
	// func (s *Server) handleAppBskyActorGetProfiles(ctx context.Context,actors []string) (*appbskytypes.ActorGetProfiles_Output, error)
	out, handleErr = s.handleAppBskyActorGetProfiles(ctx, actors)
	if handleErr != nil {
		return handleErr
	}
	return c.JSON(200, out)
}

func (s *Server) HandleAppBskyActorGetSuggestions(c echo.Context) error {
	ctx, span := otel.Tracer("server").Start(c.Request().Context(), "HandleAppBskyActorGetSuggestions")
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
	var out *appbskytypes.ActorGetSuggestions_Output
	var handleErr error
	// func (s *Server) handleAppBskyActorGetSuggestions(ctx context.Context,cursor string,limit int) (*appbskytypes.ActorGetSuggestions_Output, error)
	out, handleErr = s.handleAppBskyActorGetSuggestions(ctx, cursor, limit)
	if handleErr != nil {
		return handleErr
	}
	return c.JSON(200, out)
}

func (s *Server) HandleAppBskyActorSearchActors(c echo.Context) error {
	ctx, span := otel.Tracer("server").Start(c.Request().Context(), "HandleAppBskyActorSearchActors")
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
	term := c.QueryParam("term")
	var out *appbskytypes.ActorSearchActors_Output
	var handleErr error
	// func (s *Server) handleAppBskyActorSearchActors(ctx context.Context,cursor string,limit int,term string) (*appbskytypes.ActorSearchActors_Output, error)
	out, handleErr = s.handleAppBskyActorSearchActors(ctx, cursor, limit, term)
	if handleErr != nil {
		return handleErr
	}
	return c.JSON(200, out)
}

func (s *Server) HandleAppBskyActorSearchActorsTypeahead(c echo.Context) error {
	ctx, span := otel.Tracer("server").Start(c.Request().Context(), "HandleAppBskyActorSearchActorsTypeahead")
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
	term := c.QueryParam("term")
	var out *appbskytypes.ActorSearchActorsTypeahead_Output
	var handleErr error
	// func (s *Server) handleAppBskyActorSearchActorsTypeahead(ctx context.Context,limit int,term string) (*appbskytypes.ActorSearchActorsTypeahead_Output, error)
	out, handleErr = s.handleAppBskyActorSearchActorsTypeahead(ctx, limit, term)
	if handleErr != nil {
		return handleErr
	}
	return c.JSON(200, out)
}

func (s *Server) HandleAppBskyFeedGetAuthorFeed(c echo.Context) error {
	ctx, span := otel.Tracer("server").Start(c.Request().Context(), "HandleAppBskyFeedGetAuthorFeed")
	defer span.End()
	actor := c.QueryParam("actor")
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
	var out *appbskytypes.FeedGetAuthorFeed_Output
	var handleErr error
	// func (s *Server) handleAppBskyFeedGetAuthorFeed(ctx context.Context,actor string,cursor string,limit int) (*appbskytypes.FeedGetAuthorFeed_Output, error)
	out, handleErr = s.handleAppBskyFeedGetAuthorFeed(ctx, actor, cursor, limit)
	if handleErr != nil {
		return handleErr
	}
	return c.JSON(200, out)
}

func (s *Server) HandleAppBskyFeedGetLikes(c echo.Context) error {
	ctx, span := otel.Tracer("server").Start(c.Request().Context(), "HandleAppBskyFeedGetLikes")
	defer span.End()
	cid := c.QueryParam("cid")
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
	uri := c.QueryParam("uri")
	var out *appbskytypes.FeedGetLikes_Output
	var handleErr error
	// func (s *Server) handleAppBskyFeedGetLikes(ctx context.Context,cid string,cursor string,limit int,uri string) (*appbskytypes.FeedGetLikes_Output, error)
	out, handleErr = s.handleAppBskyFeedGetLikes(ctx, cid, cursor, limit, uri)
	if handleErr != nil {
		return handleErr
	}
	return c.JSON(200, out)
}

func (s *Server) HandleAppBskyFeedGetPostThread(c echo.Context) error {
	ctx, span := otel.Tracer("server").Start(c.Request().Context(), "HandleAppBskyFeedGetPostThread")
	defer span.End()

	var depth *int
	if p := c.QueryParam("depth"); p != "" {
		depth_val, err := strconv.Atoi(p)
		if err != nil {
			return err
		}
		depth = &depth_val
	}
	uri := c.QueryParam("uri")
	var out *appbskytypes.FeedGetPostThread_Output
	var handleErr error
	// func (s *Server) handleAppBskyFeedGetPostThread(ctx context.Context,depth *int,uri string) (*appbskytypes.FeedGetPostThread_Output, error)
	out, handleErr = s.handleAppBskyFeedGetPostThread(ctx, depth, uri)
	if handleErr != nil {
		return handleErr
	}
	return c.JSON(200, out)
}

func (s *Server) HandleAppBskyFeedGetRepostedBy(c echo.Context) error {
	ctx, span := otel.Tracer("server").Start(c.Request().Context(), "HandleAppBskyFeedGetRepostedBy")
	defer span.End()
	cid := c.QueryParam("cid")
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
	uri := c.QueryParam("uri")
	var out *appbskytypes.FeedGetRepostedBy_Output
	var handleErr error
	// func (s *Server) handleAppBskyFeedGetRepostedBy(ctx context.Context,cid string,cursor string,limit int,uri string) (*appbskytypes.FeedGetRepostedBy_Output, error)
	out, handleErr = s.handleAppBskyFeedGetRepostedBy(ctx, cid, cursor, limit, uri)
	if handleErr != nil {
		return handleErr
	}
	return c.JSON(200, out)
}

func (s *Server) HandleAppBskyFeedGetTimeline(c echo.Context) error {
	ctx, span := otel.Tracer("server").Start(c.Request().Context(), "HandleAppBskyFeedGetTimeline")
	defer span.End()
	algorithm := c.QueryParam("algorithm")
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
	var out *appbskytypes.FeedGetTimeline_Output
	var handleErr error
	// func (s *Server) handleAppBskyFeedGetTimeline(ctx context.Context,algorithm string,cursor string,limit int) (*appbskytypes.FeedGetTimeline_Output, error)
	out, handleErr = s.handleAppBskyFeedGetTimeline(ctx, algorithm, cursor, limit)
	if handleErr != nil {
		return handleErr
	}
	return c.JSON(200, out)
}

func (s *Server) HandleAppBskyGraphGetFollowers(c echo.Context) error {
	ctx, span := otel.Tracer("server").Start(c.Request().Context(), "HandleAppBskyGraphGetFollowers")
	defer span.End()
	actor := c.QueryParam("actor")
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
	var out *appbskytypes.GraphGetFollowers_Output
	var handleErr error
	// func (s *Server) handleAppBskyGraphGetFollowers(ctx context.Context,actor string,cursor string,limit int) (*appbskytypes.GraphGetFollowers_Output, error)
	out, handleErr = s.handleAppBskyGraphGetFollowers(ctx, actor, cursor, limit)
	if handleErr != nil {
		return handleErr
	}
	return c.JSON(200, out)
}

func (s *Server) HandleAppBskyGraphGetFollows(c echo.Context) error {
	ctx, span := otel.Tracer("server").Start(c.Request().Context(), "HandleAppBskyGraphGetFollows")
	defer span.End()
	actor := c.QueryParam("actor")
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
	var out *appbskytypes.GraphGetFollows_Output
	var handleErr error
	// func (s *Server) handleAppBskyGraphGetFollows(ctx context.Context,actor string,cursor string,limit int) (*appbskytypes.GraphGetFollows_Output, error)
	out, handleErr = s.handleAppBskyGraphGetFollows(ctx, actor, cursor, limit)
	if handleErr != nil {
		return handleErr
	}
	return c.JSON(200, out)
}

func (s *Server) HandleAppBskyGraphGetMutes(c echo.Context) error {
	ctx, span := otel.Tracer("server").Start(c.Request().Context(), "HandleAppBskyGraphGetMutes")
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
	var out *appbskytypes.GraphGetMutes_Output
	var handleErr error
	// func (s *Server) handleAppBskyGraphGetMutes(ctx context.Context,cursor string,limit int) (*appbskytypes.GraphGetMutes_Output, error)
	out, handleErr = s.handleAppBskyGraphGetMutes(ctx, cursor, limit)
	if handleErr != nil {
		return handleErr
	}
	return c.JSON(200, out)
}

func (s *Server) HandleAppBskyGraphMuteActor(c echo.Context) error {
	ctx, span := otel.Tracer("server").Start(c.Request().Context(), "HandleAppBskyGraphMuteActor")
	defer span.End()

	var body appbskytypes.GraphMuteActor_Input
	if err := c.Bind(&body); err != nil {
		return err
	}
	var handleErr error
	// func (s *Server) handleAppBskyGraphMuteActor(ctx context.Context,body *appbskytypes.GraphMuteActor_Input) error
	handleErr = s.handleAppBskyGraphMuteActor(ctx, &body)
	if handleErr != nil {
		return handleErr
	}
	return nil
}

func (s *Server) HandleAppBskyGraphUnmuteActor(c echo.Context) error {
	ctx, span := otel.Tracer("server").Start(c.Request().Context(), "HandleAppBskyGraphUnmuteActor")
	defer span.End()

	var body appbskytypes.GraphUnmuteActor_Input
	if err := c.Bind(&body); err != nil {
		return err
	}
	var handleErr error
	// func (s *Server) handleAppBskyGraphUnmuteActor(ctx context.Context,body *appbskytypes.GraphUnmuteActor_Input) error
	handleErr = s.handleAppBskyGraphUnmuteActor(ctx, &body)
	if handleErr != nil {
		return handleErr
	}
	return nil
}

func (s *Server) HandleAppBskyNotificationGetUnreadCount(c echo.Context) error {
	ctx, span := otel.Tracer("server").Start(c.Request().Context(), "HandleAppBskyNotificationGetUnreadCount")
	defer span.End()
	var out *appbskytypes.NotificationGetUnreadCount_Output
	var handleErr error
	// func (s *Server) handleAppBskyNotificationGetUnreadCount(ctx context.Context) (*appbskytypes.NotificationGetUnreadCount_Output, error)
	out, handleErr = s.handleAppBskyNotificationGetUnreadCount(ctx)
	if handleErr != nil {
		return handleErr
	}
	return c.JSON(200, out)
}

func (s *Server) HandleAppBskyNotificationListNotifications(c echo.Context) error {
	ctx, span := otel.Tracer("server").Start(c.Request().Context(), "HandleAppBskyNotificationListNotifications")
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
	var out *appbskytypes.NotificationListNotifications_Output
	var handleErr error
	// func (s *Server) handleAppBskyNotificationListNotifications(ctx context.Context,cursor string,limit int) (*appbskytypes.NotificationListNotifications_Output, error)
	out, handleErr = s.handleAppBskyNotificationListNotifications(ctx, cursor, limit)
	if handleErr != nil {
		return handleErr
	}
	return c.JSON(200, out)
}

func (s *Server) HandleAppBskyNotificationUpdateSeen(c echo.Context) error {
	ctx, span := otel.Tracer("server").Start(c.Request().Context(), "HandleAppBskyNotificationUpdateSeen")
	defer span.End()

	var body appbskytypes.NotificationUpdateSeen_Input
	if err := c.Bind(&body); err != nil {
		return err
	}
	var handleErr error
	// func (s *Server) handleAppBskyNotificationUpdateSeen(ctx context.Context,body *appbskytypes.NotificationUpdateSeen_Input) error
	handleErr = s.handleAppBskyNotificationUpdateSeen(ctx, &body)
	if handleErr != nil {
		return handleErr
	}
	return nil
}

func (s *Server) HandleAppBskyUnspeccedGetPopular(c echo.Context) error {
	ctx, span := otel.Tracer("server").Start(c.Request().Context(), "HandleAppBskyUnspeccedGetPopular")
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
	var out *appbskytypes.UnspeccedGetPopular_Output
	var handleErr error
	// func (s *Server) handleAppBskyUnspeccedGetPopular(ctx context.Context,cursor string,limit int) (*appbskytypes.UnspeccedGetPopular_Output, error)
	out, handleErr = s.handleAppBskyUnspeccedGetPopular(ctx, cursor, limit)
	if handleErr != nil {
		return handleErr
	}
	return c.JSON(200, out)
}

func (s *Server) RegisterHandlersComAtproto(e *echo.Echo) error {
	e.POST("/xrpc/com.atproto.admin.disableInviteCodes", s.HandleComAtprotoAdminDisableInviteCodes)
	e.GET("/xrpc/com.atproto.admin.getInviteCodes", s.HandleComAtprotoAdminGetInviteCodes)
	e.GET("/xrpc/com.atproto.admin.getModerationAction", s.HandleComAtprotoAdminGetModerationAction)
	e.GET("/xrpc/com.atproto.admin.getModerationActions", s.HandleComAtprotoAdminGetModerationActions)
	e.GET("/xrpc/com.atproto.admin.getModerationReport", s.HandleComAtprotoAdminGetModerationReport)
	e.GET("/xrpc/com.atproto.admin.getModerationReports", s.HandleComAtprotoAdminGetModerationReports)
	e.GET("/xrpc/com.atproto.admin.getRecord", s.HandleComAtprotoAdminGetRecord)
	e.GET("/xrpc/com.atproto.admin.getRepo", s.HandleComAtprotoAdminGetRepo)
	e.POST("/xrpc/com.atproto.admin.resolveModerationReports", s.HandleComAtprotoAdminResolveModerationReports)
	e.POST("/xrpc/com.atproto.admin.reverseModerationAction", s.HandleComAtprotoAdminReverseModerationAction)
	e.GET("/xrpc/com.atproto.admin.searchRepos", s.HandleComAtprotoAdminSearchRepos)
	e.POST("/xrpc/com.atproto.admin.takeModerationAction", s.HandleComAtprotoAdminTakeModerationAction)
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
	e.POST("/xrpc/com.atproto.server.createAccount", s.HandleComAtprotoServerCreateAccount)
	e.POST("/xrpc/com.atproto.server.createInviteCode", s.HandleComAtprotoServerCreateInviteCode)
	e.POST("/xrpc/com.atproto.server.createInviteCodes", s.HandleComAtprotoServerCreateInviteCodes)
	e.POST("/xrpc/com.atproto.server.createSession", s.HandleComAtprotoServerCreateSession)
	e.POST("/xrpc/com.atproto.server.deleteAccount", s.HandleComAtprotoServerDeleteAccount)
	e.POST("/xrpc/com.atproto.server.deleteSession", s.HandleComAtprotoServerDeleteSession)
	e.GET("/xrpc/com.atproto.server.describeServer", s.HandleComAtprotoServerDescribeServer)
	e.GET("/xrpc/com.atproto.server.getAccountInviteCodes", s.HandleComAtprotoServerGetAccountInviteCodes)
	e.GET("/xrpc/com.atproto.server.getSession", s.HandleComAtprotoServerGetSession)
	e.POST("/xrpc/com.atproto.server.refreshSession", s.HandleComAtprotoServerRefreshSession)
	e.POST("/xrpc/com.atproto.server.requestAccountDelete", s.HandleComAtprotoServerRequestAccountDelete)
	e.POST("/xrpc/com.atproto.server.requestPasswordReset", s.HandleComAtprotoServerRequestPasswordReset)
	e.POST("/xrpc/com.atproto.server.resetPassword", s.HandleComAtprotoServerResetPassword)
	e.GET("/xrpc/com.atproto.sync.getBlob", s.HandleComAtprotoSyncGetBlob)
	e.GET("/xrpc/com.atproto.sync.getBlocks", s.HandleComAtprotoSyncGetBlocks)
	e.GET("/xrpc/com.atproto.sync.getCheckout", s.HandleComAtprotoSyncGetCheckout)
	e.GET("/xrpc/com.atproto.sync.getCommitPath", s.HandleComAtprotoSyncGetCommitPath)
	e.GET("/xrpc/com.atproto.sync.getHead", s.HandleComAtprotoSyncGetHead)
	e.GET("/xrpc/com.atproto.sync.getRecord", s.HandleComAtprotoSyncGetRecord)
	e.GET("/xrpc/com.atproto.sync.getRepo", s.HandleComAtprotoSyncGetRepo)
	e.GET("/xrpc/com.atproto.sync.listBlobs", s.HandleComAtprotoSyncListBlobs)
	e.GET("/xrpc/com.atproto.sync.listRepos", s.HandleComAtprotoSyncListRepos)
	e.GET("/xrpc/com.atproto.sync.notifyOfUpdate", s.HandleComAtprotoSyncNotifyOfUpdate)
	e.GET("/xrpc/com.atproto.sync.requestCrawl", s.HandleComAtprotoSyncRequestCrawl)
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

func (s *Server) HandleComAtprotoAdminGetModerationAction(c echo.Context) error {
	ctx, span := otel.Tracer("server").Start(c.Request().Context(), "HandleComAtprotoAdminGetModerationAction")
	defer span.End()

	id, err := strconv.Atoi(c.QueryParam("id"))
	if err != nil {
		return err
	}
	var out *comatprototypes.AdminDefs_ActionViewDetail
	var handleErr error
	// func (s *Server) handleComAtprotoAdminGetModerationAction(ctx context.Context,id int) (*comatprototypes.AdminDefs_ActionViewDetail, error)
	out, handleErr = s.handleComAtprotoAdminGetModerationAction(ctx, id)
	if handleErr != nil {
		return handleErr
	}
	return c.JSON(200, out)
}

func (s *Server) HandleComAtprotoAdminGetModerationActions(c echo.Context) error {
	ctx, span := otel.Tracer("server").Start(c.Request().Context(), "HandleComAtprotoAdminGetModerationActions")
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
	subject := c.QueryParam("subject")
	var out *comatprototypes.AdminGetModerationActions_Output
	var handleErr error
	// func (s *Server) handleComAtprotoAdminGetModerationActions(ctx context.Context,cursor string,limit int,subject string) (*comatprototypes.AdminGetModerationActions_Output, error)
	out, handleErr = s.handleComAtprotoAdminGetModerationActions(ctx, cursor, limit, subject)
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
	var out *comatprototypes.AdminDefs_ReportViewDetail
	var handleErr error
	// func (s *Server) handleComAtprotoAdminGetModerationReport(ctx context.Context,id int) (*comatprototypes.AdminDefs_ReportViewDetail, error)
	out, handleErr = s.handleComAtprotoAdminGetModerationReport(ctx, id)
	if handleErr != nil {
		return handleErr
	}
	return c.JSON(200, out)
}

func (s *Server) HandleComAtprotoAdminGetModerationReports(c echo.Context) error {
	ctx, span := otel.Tracer("server").Start(c.Request().Context(), "HandleComAtprotoAdminGetModerationReports")
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

	var resolved *bool
	if p := c.QueryParam("resolved"); p != "" {
		resolved_val, err := strconv.ParseBool(p)
		if err != nil {
			return err
		}
		resolved = &resolved_val
	}
	subject := c.QueryParam("subject")
	var out *comatprototypes.AdminGetModerationReports_Output
	var handleErr error
	// func (s *Server) handleComAtprotoAdminGetModerationReports(ctx context.Context,cursor string,limit int,resolved *bool,subject string) (*comatprototypes.AdminGetModerationReports_Output, error)
	out, handleErr = s.handleComAtprotoAdminGetModerationReports(ctx, cursor, limit, resolved, subject)
	if handleErr != nil {
		return handleErr
	}
	return c.JSON(200, out)
}

func (s *Server) HandleComAtprotoAdminGetRecord(c echo.Context) error {
	ctx, span := otel.Tracer("server").Start(c.Request().Context(), "HandleComAtprotoAdminGetRecord")
	defer span.End()
	cid := c.QueryParam("cid")
	uri := c.QueryParam("uri")
	var out *comatprototypes.AdminDefs_RecordViewDetail
	var handleErr error
	// func (s *Server) handleComAtprotoAdminGetRecord(ctx context.Context,cid string,uri string) (*comatprototypes.AdminDefs_RecordViewDetail, error)
	out, handleErr = s.handleComAtprotoAdminGetRecord(ctx, cid, uri)
	if handleErr != nil {
		return handleErr
	}
	return c.JSON(200, out)
}

func (s *Server) HandleComAtprotoAdminGetRepo(c echo.Context) error {
	ctx, span := otel.Tracer("server").Start(c.Request().Context(), "HandleComAtprotoAdminGetRepo")
	defer span.End()
	did := c.QueryParam("did")
	var out *comatprototypes.AdminDefs_RepoViewDetail
	var handleErr error
	// func (s *Server) handleComAtprotoAdminGetRepo(ctx context.Context,did string) (*comatprototypes.AdminDefs_RepoViewDetail, error)
	out, handleErr = s.handleComAtprotoAdminGetRepo(ctx, did)
	if handleErr != nil {
		return handleErr
	}
	return c.JSON(200, out)
}

func (s *Server) HandleComAtprotoAdminResolveModerationReports(c echo.Context) error {
	ctx, span := otel.Tracer("server").Start(c.Request().Context(), "HandleComAtprotoAdminResolveModerationReports")
	defer span.End()

	var body comatprototypes.AdminResolveModerationReports_Input
	if err := c.Bind(&body); err != nil {
		return err
	}
	var out *comatprototypes.AdminDefs_ActionView
	var handleErr error
	// func (s *Server) handleComAtprotoAdminResolveModerationReports(ctx context.Context,body *comatprototypes.AdminResolveModerationReports_Input) (*comatprototypes.AdminDefs_ActionView, error)
	out, handleErr = s.handleComAtprotoAdminResolveModerationReports(ctx, &body)
	if handleErr != nil {
		return handleErr
	}
	return c.JSON(200, out)
}

func (s *Server) HandleComAtprotoAdminReverseModerationAction(c echo.Context) error {
	ctx, span := otel.Tracer("server").Start(c.Request().Context(), "HandleComAtprotoAdminReverseModerationAction")
	defer span.End()

	var body comatprototypes.AdminReverseModerationAction_Input
	if err := c.Bind(&body); err != nil {
		return err
	}
	var out *comatprototypes.AdminDefs_ActionView
	var handleErr error
	// func (s *Server) handleComAtprotoAdminReverseModerationAction(ctx context.Context,body *comatprototypes.AdminReverseModerationAction_Input) (*comatprototypes.AdminDefs_ActionView, error)
	out, handleErr = s.handleComAtprotoAdminReverseModerationAction(ctx, &body)
	if handleErr != nil {
		return handleErr
	}
	return c.JSON(200, out)
}

func (s *Server) HandleComAtprotoAdminSearchRepos(c echo.Context) error {
	ctx, span := otel.Tracer("server").Start(c.Request().Context(), "HandleComAtprotoAdminSearchRepos")
	defer span.End()
	cursor := c.QueryParam("cursor")
	invitedBy := c.QueryParam("invitedBy")

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
	term := c.QueryParam("term")
	var out *comatprototypes.AdminSearchRepos_Output
	var handleErr error
	// func (s *Server) handleComAtprotoAdminSearchRepos(ctx context.Context,cursor string,invitedBy string,limit int,term string) (*comatprototypes.AdminSearchRepos_Output, error)
	out, handleErr = s.handleComAtprotoAdminSearchRepos(ctx, cursor, invitedBy, limit, term)
	if handleErr != nil {
		return handleErr
	}
	return c.JSON(200, out)
}

func (s *Server) HandleComAtprotoAdminTakeModerationAction(c echo.Context) error {
	ctx, span := otel.Tracer("server").Start(c.Request().Context(), "HandleComAtprotoAdminTakeModerationAction")
	defer span.End()

	var body comatprototypes.AdminTakeModerationAction_Input
	if err := c.Bind(&body); err != nil {
		return err
	}
	var out *comatprototypes.AdminDefs_ActionView
	var handleErr error
	// func (s *Server) handleComAtprotoAdminTakeModerationAction(ctx context.Context,body *comatprototypes.AdminTakeModerationAction_Input) (*comatprototypes.AdminDefs_ActionView, error)
	out, handleErr = s.handleComAtprotoAdminTakeModerationAction(ctx, &body)
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
	commit := c.QueryParam("commit")
	did := c.QueryParam("did")
	var out io.Reader
	var handleErr error
	// func (s *Server) handleComAtprotoSyncGetCheckout(ctx context.Context,commit string,did string) (io.Reader, error)
	out, handleErr = s.handleComAtprotoSyncGetCheckout(ctx, commit, did)
	if handleErr != nil {
		return handleErr
	}
	return c.Stream(200, "application/vnd.ipld.car", out)
}

func (s *Server) HandleComAtprotoSyncGetCommitPath(c echo.Context) error {
	ctx, span := otel.Tracer("server").Start(c.Request().Context(), "HandleComAtprotoSyncGetCommitPath")
	defer span.End()
	did := c.QueryParam("did")
	earliest := c.QueryParam("earliest")
	latest := c.QueryParam("latest")
	var out *comatprototypes.SyncGetCommitPath_Output
	var handleErr error
	// func (s *Server) handleComAtprotoSyncGetCommitPath(ctx context.Context,did string,earliest string,latest string) (*comatprototypes.SyncGetCommitPath_Output, error)
	out, handleErr = s.handleComAtprotoSyncGetCommitPath(ctx, did, earliest, latest)
	if handleErr != nil {
		return handleErr
	}
	return c.JSON(200, out)
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

func (s *Server) HandleComAtprotoSyncListBlobs(c echo.Context) error {
	ctx, span := otel.Tracer("server").Start(c.Request().Context(), "HandleComAtprotoSyncListBlobs")
	defer span.End()
	did := c.QueryParam("did")
	earliest := c.QueryParam("earliest")
	latest := c.QueryParam("latest")
	var out *comatprototypes.SyncListBlobs_Output
	var handleErr error
	// func (s *Server) handleComAtprotoSyncListBlobs(ctx context.Context,did string,earliest string,latest string) (*comatprototypes.SyncListBlobs_Output, error)
	out, handleErr = s.handleComAtprotoSyncListBlobs(ctx, did, earliest, latest)
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
	hostname := c.QueryParam("hostname")
	var handleErr error
	// func (s *Server) handleComAtprotoSyncNotifyOfUpdate(ctx context.Context,hostname string) error
	handleErr = s.handleComAtprotoSyncNotifyOfUpdate(ctx, hostname)
	if handleErr != nil {
		return handleErr
	}
	return nil
}

func (s *Server) HandleComAtprotoSyncRequestCrawl(c echo.Context) error {
	ctx, span := otel.Tracer("server").Start(c.Request().Context(), "HandleComAtprotoSyncRequestCrawl")
	defer span.End()
	hostname := c.QueryParam("hostname")
	var handleErr error
	// func (s *Server) handleComAtprotoSyncRequestCrawl(ctx context.Context,hostname string) error
	handleErr = s.handleComAtprotoSyncRequestCrawl(ctx, hostname)
	if handleErr != nil {
		return handleErr
	}
	return nil
}
