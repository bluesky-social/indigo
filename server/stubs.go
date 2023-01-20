package schemagen

import (
	"io"
	"strconv"

	comatprototypes "github.com/bluesky-social/indigo/api/atproto"
	appbskytypes "github.com/bluesky-social/indigo/api/bsky"
	"github.com/labstack/echo/v4"
	"go.opentelemetry.io/otel"
)

func (s *Server) RegisterHandlersAppBsky(e *echo.Echo) error {
	e.POST("/xrpc/app.bsky.actor.createScene", s.HandleAppBskyActorCreateScene)
	e.GET("/xrpc/app.bsky.actor.getProfile", s.HandleAppBskyActorGetProfile)
	e.GET("/xrpc/app.bsky.actor.getSuggestions", s.HandleAppBskyActorGetSuggestions)
	e.GET("/xrpc/app.bsky.actor.search", s.HandleAppBskyActorSearch)
	e.GET("/xrpc/app.bsky.actor.searchTypeahead", s.HandleAppBskyActorSearchTypeahead)
	e.POST("/xrpc/app.bsky.actor.updateProfile", s.HandleAppBskyActorUpdateProfile)
	e.GET("/xrpc/app.bsky.feed.getAuthorFeed", s.HandleAppBskyFeedGetAuthorFeed)
	e.GET("/xrpc/app.bsky.feed.getPostThread", s.HandleAppBskyFeedGetPostThread)
	e.GET("/xrpc/app.bsky.feed.getRepostedBy", s.HandleAppBskyFeedGetRepostedBy)
	e.GET("/xrpc/app.bsky.feed.getTimeline", s.HandleAppBskyFeedGetTimeline)
	e.GET("/xrpc/app.bsky.feed.getVotes", s.HandleAppBskyFeedGetVotes)
	e.POST("/xrpc/app.bsky.feed.setVote", s.HandleAppBskyFeedSetVote)
	e.GET("/xrpc/app.bsky.graph.getAssertions", s.HandleAppBskyGraphGetAssertions)
	e.GET("/xrpc/app.bsky.graph.getFollowers", s.HandleAppBskyGraphGetFollowers)
	e.GET("/xrpc/app.bsky.graph.getFollows", s.HandleAppBskyGraphGetFollows)
	e.GET("/xrpc/app.bsky.graph.getMembers", s.HandleAppBskyGraphGetMembers)
	e.GET("/xrpc/app.bsky.graph.getMemberships", s.HandleAppBskyGraphGetMemberships)
	e.GET("/xrpc/app.bsky.graph.getMutes", s.HandleAppBskyGraphGetMutes)
	e.POST("/xrpc/app.bsky.graph.mute", s.HandleAppBskyGraphMute)
	e.POST("/xrpc/app.bsky.graph.unmute", s.HandleAppBskyGraphUnmute)
	e.GET("/xrpc/app.bsky.notification.getCount", s.HandleAppBskyNotificationGetCount)
	e.GET("/xrpc/app.bsky.notification.list", s.HandleAppBskyNotificationList)
	e.POST("/xrpc/app.bsky.notification.updateSeen", s.HandleAppBskyNotificationUpdateSeen)

	e.POST("/xrpc/com.atproto.peering.follow", s.HandleComAtprotoPeeringFollow)
	e.POST("/xrpc/com.atproto.peering.init", s.HandleComAtprotoPeeringInit)
	e.GET("/xrpc/com.atproto.peering.list", s.HandleComAtprotoPeeringList)
	e.POST("/xrpc/com.atproto.peering.propose", s.HandleComAtprotoPeeringPropose)
	return nil
}

func (s *Server) HandleAppBskyActorCreateScene(c echo.Context) error {
	ctx, span := otel.Tracer("server").Start(c.Request().Context(), "HandleAppBskyActorCreateScene")
	defer span.End()

	var body appbskytypes.ActorCreateScene_Input
	if err := c.Bind(&body); err != nil {
		return err
	}
	var out *appbskytypes.ActorCreateScene_Output
	var handleErr error
	// func (s *Server) handleAppBskyActorCreateScene(ctx context.Context,body *appbskytypes.ActorCreateScene_Input) (*appbskytypes.ActorCreateScene_Output, error)
	out, handleErr = s.handleAppBskyActorCreateScene(ctx, &body)
	if handleErr != nil {
		return handleErr
	}
	return c.JSON(200, out)
}

func (s *Server) HandleAppBskyActorGetProfile(c echo.Context) error {
	ctx, span := otel.Tracer("server").Start(c.Request().Context(), "HandleAppBskyActorGetProfile")
	defer span.End()
	actor := c.QueryParam("actor")
	var out *appbskytypes.ActorGetProfile_Output
	var handleErr error
	// func (s *Server) handleAppBskyActorGetProfile(ctx context.Context,actor string) (*appbskytypes.ActorGetProfile_Output, error)
	out, handleErr = s.handleAppBskyActorGetProfile(ctx, actor)
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

func (s *Server) HandleAppBskyActorSearch(c echo.Context) error {
	ctx, span := otel.Tracer("server").Start(c.Request().Context(), "HandleAppBskyActorSearch")
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
	term := c.QueryParam("term")
	var out *appbskytypes.ActorSearch_Output
	var handleErr error
	// func (s *Server) handleAppBskyActorSearch(ctx context.Context,before string,limit int,term string) (*appbskytypes.ActorSearch_Output, error)
	out, handleErr = s.handleAppBskyActorSearch(ctx, before, limit, term)
	if handleErr != nil {
		return handleErr
	}
	return c.JSON(200, out)
}

func (s *Server) HandleAppBskyActorSearchTypeahead(c echo.Context) error {
	ctx, span := otel.Tracer("server").Start(c.Request().Context(), "HandleAppBskyActorSearchTypeahead")
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
	var out *appbskytypes.ActorSearchTypeahead_Output
	var handleErr error
	// func (s *Server) handleAppBskyActorSearchTypeahead(ctx context.Context,limit int,term string) (*appbskytypes.ActorSearchTypeahead_Output, error)
	out, handleErr = s.handleAppBskyActorSearchTypeahead(ctx, limit, term)
	if handleErr != nil {
		return handleErr
	}
	return c.JSON(200, out)
}

func (s *Server) HandleAppBskyActorUpdateProfile(c echo.Context) error {
	ctx, span := otel.Tracer("server").Start(c.Request().Context(), "HandleAppBskyActorUpdateProfile")
	defer span.End()

	var body appbskytypes.ActorUpdateProfile_Input
	if err := c.Bind(&body); err != nil {
		return err
	}
	var out *appbskytypes.ActorUpdateProfile_Output
	var handleErr error
	// func (s *Server) handleAppBskyActorUpdateProfile(ctx context.Context,body *appbskytypes.ActorUpdateProfile_Input) (*appbskytypes.ActorUpdateProfile_Output, error)
	out, handleErr = s.handleAppBskyActorUpdateProfile(ctx, &body)
	if handleErr != nil {
		return handleErr
	}
	return c.JSON(200, out)
}

func (s *Server) HandleAppBskyFeedGetAuthorFeed(c echo.Context) error {
	ctx, span := otel.Tracer("server").Start(c.Request().Context(), "HandleAppBskyFeedGetAuthorFeed")
	defer span.End()
	author := c.QueryParam("author")
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
	var out *appbskytypes.FeedGetAuthorFeed_Output
	var handleErr error
	// func (s *Server) handleAppBskyFeedGetAuthorFeed(ctx context.Context,author string,before string,limit int) (*appbskytypes.FeedGetAuthorFeed_Output, error)
	out, handleErr = s.handleAppBskyFeedGetAuthorFeed(ctx, author, before, limit)
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
	before := c.QueryParam("before")
	cid := c.QueryParam("cid")

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
	// func (s *Server) handleAppBskyFeedGetRepostedBy(ctx context.Context,before string,cid string,limit int,uri string) (*appbskytypes.FeedGetRepostedBy_Output, error)
	out, handleErr = s.handleAppBskyFeedGetRepostedBy(ctx, before, cid, limit, uri)
	if handleErr != nil {
		return handleErr
	}
	return c.JSON(200, out)
}

func (s *Server) HandleAppBskyFeedGetTimeline(c echo.Context) error {
	ctx, span := otel.Tracer("server").Start(c.Request().Context(), "HandleAppBskyFeedGetTimeline")
	defer span.End()
	algorithm := c.QueryParam("algorithm")
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
	var out *appbskytypes.FeedGetTimeline_Output
	var handleErr error
	// func (s *Server) handleAppBskyFeedGetTimeline(ctx context.Context,algorithm string,before string,limit int) (*appbskytypes.FeedGetTimeline_Output, error)
	out, handleErr = s.handleAppBskyFeedGetTimeline(ctx, algorithm, before, limit)
	if handleErr != nil {
		return handleErr
	}
	return c.JSON(200, out)
}

func (s *Server) HandleAppBskyFeedGetVotes(c echo.Context) error {
	ctx, span := otel.Tracer("server").Start(c.Request().Context(), "HandleAppBskyFeedGetVotes")
	defer span.End()
	before := c.QueryParam("before")
	cid := c.QueryParam("cid")
	direction := c.QueryParam("direction")

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
	var out *appbskytypes.FeedGetVotes_Output
	var handleErr error
	// func (s *Server) handleAppBskyFeedGetVotes(ctx context.Context,before string,cid string,direction string,limit int,uri string) (*appbskytypes.FeedGetVotes_Output, error)
	out, handleErr = s.handleAppBskyFeedGetVotes(ctx, before, cid, direction, limit, uri)
	if handleErr != nil {
		return handleErr
	}
	return c.JSON(200, out)
}

func (s *Server) HandleAppBskyFeedSetVote(c echo.Context) error {
	ctx, span := otel.Tracer("server").Start(c.Request().Context(), "HandleAppBskyFeedSetVote")
	defer span.End()

	var body appbskytypes.FeedSetVote_Input
	if err := c.Bind(&body); err != nil {
		return err
	}
	var out *appbskytypes.FeedSetVote_Output
	var handleErr error
	// func (s *Server) handleAppBskyFeedSetVote(ctx context.Context,body *appbskytypes.FeedSetVote_Input) (*appbskytypes.FeedSetVote_Output, error)
	out, handleErr = s.handleAppBskyFeedSetVote(ctx, &body)
	if handleErr != nil {
		return handleErr
	}
	return c.JSON(200, out)
}

func (s *Server) HandleAppBskyGraphGetAssertions(c echo.Context) error {
	ctx, span := otel.Tracer("server").Start(c.Request().Context(), "HandleAppBskyGraphGetAssertions")
	defer span.End()
	assertion := c.QueryParam("assertion")
	author := c.QueryParam("author")
	before := c.QueryParam("before")

	var confirmed *bool
	if p := c.QueryParam("confirmed"); p != "" {
		confirmed_val, err := strconv.ParseBool(p)
		if err != nil {
			return err
		}
		confirmed = &confirmed_val
	}

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
	var out *appbskytypes.GraphGetAssertions_Output
	var handleErr error
	// func (s *Server) handleAppBskyGraphGetAssertions(ctx context.Context,assertion string,author string,before string,confirmed *bool,limit int,subject string) (*appbskytypes.GraphGetAssertions_Output, error)
	out, handleErr = s.handleAppBskyGraphGetAssertions(ctx, assertion, author, before, confirmed, limit, subject)
	if handleErr != nil {
		return handleErr
	}
	return c.JSON(200, out)
}

func (s *Server) HandleAppBskyGraphGetFollowers(c echo.Context) error {
	ctx, span := otel.Tracer("server").Start(c.Request().Context(), "HandleAppBskyGraphGetFollowers")
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
	user := c.QueryParam("user")
	var out *appbskytypes.GraphGetFollowers_Output
	var handleErr error
	// func (s *Server) handleAppBskyGraphGetFollowers(ctx context.Context,before string,limit int,user string) (*appbskytypes.GraphGetFollowers_Output, error)
	out, handleErr = s.handleAppBskyGraphGetFollowers(ctx, before, limit, user)
	if handleErr != nil {
		return handleErr
	}
	return c.JSON(200, out)
}

func (s *Server) HandleAppBskyGraphGetFollows(c echo.Context) error {
	ctx, span := otel.Tracer("server").Start(c.Request().Context(), "HandleAppBskyGraphGetFollows")
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
	user := c.QueryParam("user")
	var out *appbskytypes.GraphGetFollows_Output
	var handleErr error
	// func (s *Server) handleAppBskyGraphGetFollows(ctx context.Context,before string,limit int,user string) (*appbskytypes.GraphGetFollows_Output, error)
	out, handleErr = s.handleAppBskyGraphGetFollows(ctx, before, limit, user)
	if handleErr != nil {
		return handleErr
	}
	return c.JSON(200, out)
}

func (s *Server) HandleAppBskyGraphGetMembers(c echo.Context) error {
	ctx, span := otel.Tracer("server").Start(c.Request().Context(), "HandleAppBskyGraphGetMembers")
	defer span.End()
	actor := c.QueryParam("actor")
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
	var out *appbskytypes.GraphGetMembers_Output
	var handleErr error
	// func (s *Server) handleAppBskyGraphGetMembers(ctx context.Context,actor string,before string,limit int) (*appbskytypes.GraphGetMembers_Output, error)
	out, handleErr = s.handleAppBskyGraphGetMembers(ctx, actor, before, limit)
	if handleErr != nil {
		return handleErr
	}
	return c.JSON(200, out)
}

func (s *Server) HandleAppBskyGraphGetMemberships(c echo.Context) error {
	ctx, span := otel.Tracer("server").Start(c.Request().Context(), "HandleAppBskyGraphGetMemberships")
	defer span.End()
	actor := c.QueryParam("actor")
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
	var out *appbskytypes.GraphGetMemberships_Output
	var handleErr error
	// func (s *Server) handleAppBskyGraphGetMemberships(ctx context.Context,actor string,before string,limit int) (*appbskytypes.GraphGetMemberships_Output, error)
	out, handleErr = s.handleAppBskyGraphGetMemberships(ctx, actor, before, limit)
	if handleErr != nil {
		return handleErr
	}
	return c.JSON(200, out)
}

func (s *Server) HandleAppBskyGraphGetMutes(c echo.Context) error {
	ctx, span := otel.Tracer("server").Start(c.Request().Context(), "HandleAppBskyGraphGetMutes")
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
	var out *appbskytypes.GraphGetMutes_Output
	var handleErr error
	// func (s *Server) handleAppBskyGraphGetMutes(ctx context.Context,before string,limit int) (*appbskytypes.GraphGetMutes_Output, error)
	out, handleErr = s.handleAppBskyGraphGetMutes(ctx, before, limit)
	if handleErr != nil {
		return handleErr
	}
	return c.JSON(200, out)
}

func (s *Server) HandleAppBskyGraphMute(c echo.Context) error {
	ctx, span := otel.Tracer("server").Start(c.Request().Context(), "HandleAppBskyGraphMute")
	defer span.End()

	var body appbskytypes.GraphMute_Input
	if err := c.Bind(&body); err != nil {
		return err
	}
	var handleErr error
	// func (s *Server) handleAppBskyGraphMute(ctx context.Context,body *appbskytypes.GraphMute_Input) error
	handleErr = s.handleAppBskyGraphMute(ctx, &body)
	if handleErr != nil {
		return handleErr
	}
	return nil
}

func (s *Server) HandleAppBskyGraphUnmute(c echo.Context) error {
	ctx, span := otel.Tracer("server").Start(c.Request().Context(), "HandleAppBskyGraphUnmute")
	defer span.End()

	var body appbskytypes.GraphUnmute_Input
	if err := c.Bind(&body); err != nil {
		return err
	}
	var handleErr error
	// func (s *Server) handleAppBskyGraphUnmute(ctx context.Context,body *appbskytypes.GraphUnmute_Input) error
	handleErr = s.handleAppBskyGraphUnmute(ctx, &body)
	if handleErr != nil {
		return handleErr
	}
	return nil
}

func (s *Server) HandleAppBskyNotificationGetCount(c echo.Context) error {
	ctx, span := otel.Tracer("server").Start(c.Request().Context(), "HandleAppBskyNotificationGetCount")
	defer span.End()
	var out *appbskytypes.NotificationGetCount_Output
	var handleErr error
	// func (s *Server) handleAppBskyNotificationGetCount(ctx context.Context) (*appbskytypes.NotificationGetCount_Output, error)
	out, handleErr = s.handleAppBskyNotificationGetCount(ctx)
	if handleErr != nil {
		return handleErr
	}
	return c.JSON(200, out)
}

func (s *Server) HandleAppBskyNotificationList(c echo.Context) error {
	ctx, span := otel.Tracer("server").Start(c.Request().Context(), "HandleAppBskyNotificationList")
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
	var out *appbskytypes.NotificationList_Output
	var handleErr error
	// func (s *Server) handleAppBskyNotificationList(ctx context.Context,before string,limit int) (*appbskytypes.NotificationList_Output, error)
	out, handleErr = s.handleAppBskyNotificationList(ctx, before, limit)
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

func (s *Server) RegisterHandlersComAtproto(e *echo.Echo) error {
	e.POST("/xrpc/com.atproto.account.create", s.HandleComAtprotoAccountCreate)
	e.POST("/xrpc/com.atproto.account.createInviteCode", s.HandleComAtprotoAccountCreateInviteCode)
	e.POST("/xrpc/com.atproto.account.delete", s.HandleComAtprotoAccountDelete)
	e.GET("/xrpc/com.atproto.account.get", s.HandleComAtprotoAccountGet)
	e.POST("/xrpc/com.atproto.account.requestPasswordReset", s.HandleComAtprotoAccountRequestPasswordReset)
	e.POST("/xrpc/com.atproto.account.resetPassword", s.HandleComAtprotoAccountResetPassword)
	e.POST("/xrpc/com.atproto.blob.upload", s.HandleComAtprotoBlobUpload)
	e.GET("/xrpc/com.atproto.handle.resolve", s.HandleComAtprotoHandleResolve)
	e.POST("/xrpc/com.atproto.repo.batchWrite", s.HandleComAtprotoRepoBatchWrite)
	e.POST("/xrpc/com.atproto.repo.createRecord", s.HandleComAtprotoRepoCreateRecord)
	e.POST("/xrpc/com.atproto.repo.deleteRecord", s.HandleComAtprotoRepoDeleteRecord)
	e.GET("/xrpc/com.atproto.repo.describe", s.HandleComAtprotoRepoDescribe)
	e.GET("/xrpc/com.atproto.repo.getRecord", s.HandleComAtprotoRepoGetRecord)
	e.GET("/xrpc/com.atproto.repo.listRecords", s.HandleComAtprotoRepoListRecords)
	e.POST("/xrpc/com.atproto.repo.putRecord", s.HandleComAtprotoRepoPutRecord)
	e.GET("/xrpc/com.atproto.server.getAccountsConfig", s.HandleComAtprotoServerGetAccountsConfig)
	e.POST("/xrpc/com.atproto.session.create", s.HandleComAtprotoSessionCreate)
	e.POST("/xrpc/com.atproto.session.delete", s.HandleComAtprotoSessionDelete)
	e.GET("/xrpc/com.atproto.session.get", s.HandleComAtprotoSessionGet)
	e.POST("/xrpc/com.atproto.session.refresh", s.HandleComAtprotoSessionRefresh)
	e.GET("/xrpc/com.atproto.sync.getRepo", s.HandleComAtprotoSyncGetRepo)
	e.GET("/xrpc/com.atproto.sync.getRoot", s.HandleComAtprotoSyncGetRoot)
	e.POST("/xrpc/com.atproto.sync.updateRepo", s.HandleComAtprotoSyncUpdateRepo)
	return nil
}

func (s *Server) HandleComAtprotoAccountCreate(c echo.Context) error {
	ctx, span := otel.Tracer("server").Start(c.Request().Context(), "HandleComAtprotoAccountCreate")
	defer span.End()

	var body comatprototypes.AccountCreate_Input
	if err := c.Bind(&body); err != nil {
		return err
	}
	var out *comatprototypes.AccountCreate_Output
	var handleErr error
	// func (s *Server) handleComAtprotoAccountCreate(ctx context.Context,body *comatprototypes.AccountCreate_Input) (*comatprototypes.AccountCreate_Output, error)
	out, handleErr = s.handleComAtprotoAccountCreate(ctx, &body)
	if handleErr != nil {
		return handleErr
	}
	return c.JSON(200, out)
}

func (s *Server) HandleComAtprotoAccountCreateInviteCode(c echo.Context) error {
	ctx, span := otel.Tracer("server").Start(c.Request().Context(), "HandleComAtprotoAccountCreateInviteCode")
	defer span.End()

	var body comatprototypes.AccountCreateInviteCode_Input
	if err := c.Bind(&body); err != nil {
		return err
	}
	var out *comatprototypes.AccountCreateInviteCode_Output
	var handleErr error
	// func (s *Server) handleComAtprotoAccountCreateInviteCode(ctx context.Context,body *comatprototypes.AccountCreateInviteCode_Input) (*comatprototypes.AccountCreateInviteCode_Output, error)
	out, handleErr = s.handleComAtprotoAccountCreateInviteCode(ctx, &body)
	if handleErr != nil {
		return handleErr
	}
	return c.JSON(200, out)
}

func (s *Server) HandleComAtprotoAccountDelete(c echo.Context) error {
	ctx, span := otel.Tracer("server").Start(c.Request().Context(), "HandleComAtprotoAccountDelete")
	defer span.End()
	var handleErr error
	// func (s *Server) handleComAtprotoAccountDelete(ctx context.Context) error
	handleErr = s.handleComAtprotoAccountDelete(ctx)
	if handleErr != nil {
		return handleErr
	}
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

func (s *Server) HandleComAtprotoAccountRequestPasswordReset(c echo.Context) error {
	ctx, span := otel.Tracer("server").Start(c.Request().Context(), "HandleComAtprotoAccountRequestPasswordReset")
	defer span.End()

	var body comatprototypes.AccountRequestPasswordReset_Input
	if err := c.Bind(&body); err != nil {
		return err
	}
	var handleErr error
	// func (s *Server) handleComAtprotoAccountRequestPasswordReset(ctx context.Context,body *comatprototypes.AccountRequestPasswordReset_Input) error
	handleErr = s.handleComAtprotoAccountRequestPasswordReset(ctx, &body)
	if handleErr != nil {
		return handleErr
	}
	return nil
}

func (s *Server) HandleComAtprotoAccountResetPassword(c echo.Context) error {
	ctx, span := otel.Tracer("server").Start(c.Request().Context(), "HandleComAtprotoAccountResetPassword")
	defer span.End()

	var body comatprototypes.AccountResetPassword_Input
	if err := c.Bind(&body); err != nil {
		return err
	}
	var handleErr error
	// func (s *Server) handleComAtprotoAccountResetPassword(ctx context.Context,body *comatprototypes.AccountResetPassword_Input) error
	handleErr = s.handleComAtprotoAccountResetPassword(ctx, &body)
	if handleErr != nil {
		return handleErr
	}
	return nil
}

func (s *Server) HandleComAtprotoBlobUpload(c echo.Context) error {
	ctx, span := otel.Tracer("server").Start(c.Request().Context(), "HandleComAtprotoBlobUpload")
	defer span.End()
	body := c.Request().Body
	contentType := c.Request().Header.Get("Content-Type")
	var out *comatprototypes.BlobUpload_Output
	var handleErr error
	// func (s *Server) handleComAtprotoBlobUpload(ctx context.Context,r io.Reader,contentType string) (*comatprototypes.BlobUpload_Output, error)
	out, handleErr = s.handleComAtprotoBlobUpload(ctx, body, contentType)
	if handleErr != nil {
		return handleErr
	}
	return c.JSON(200, out)
}

func (s *Server) HandleComAtprotoHandleResolve(c echo.Context) error {
	ctx, span := otel.Tracer("server").Start(c.Request().Context(), "HandleComAtprotoHandleResolve")
	defer span.End()
	handle := c.QueryParam("handle")
	var out *comatprototypes.HandleResolve_Output
	var handleErr error
	// func (s *Server) handleComAtprotoHandleResolve(ctx context.Context,handle string) (*comatprototypes.HandleResolve_Output, error)
	out, handleErr = s.handleComAtprotoHandleResolve(ctx, handle)
	if handleErr != nil {
		return handleErr
	}
	return c.JSON(200, out)
}

func (s *Server) HandleComAtprotoRepoBatchWrite(c echo.Context) error {
	ctx, span := otel.Tracer("server").Start(c.Request().Context(), "HandleComAtprotoRepoBatchWrite")
	defer span.End()

	var body comatprototypes.RepoBatchWrite_Input
	if err := c.Bind(&body); err != nil {
		return err
	}
	var handleErr error
	// func (s *Server) handleComAtprotoRepoBatchWrite(ctx context.Context,body *comatprototypes.RepoBatchWrite_Input) error
	handleErr = s.handleComAtprotoRepoBatchWrite(ctx, &body)
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

func (s *Server) HandleComAtprotoRepoDescribe(c echo.Context) error {
	ctx, span := otel.Tracer("server").Start(c.Request().Context(), "HandleComAtprotoRepoDescribe")
	defer span.End()
	user := c.QueryParam("user")
	var out *comatprototypes.RepoDescribe_Output
	var handleErr error
	// func (s *Server) handleComAtprotoRepoDescribe(ctx context.Context,user string) (*comatprototypes.RepoDescribe_Output, error)
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
	var out *comatprototypes.RepoGetRecord_Output
	var handleErr error
	// func (s *Server) handleComAtprotoRepoGetRecord(ctx context.Context,cid string,collection string,rkey string,user string) (*comatprototypes.RepoGetRecord_Output, error)
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
	var out *comatprototypes.RepoListRecords_Output
	var handleErr error
	// func (s *Server) handleComAtprotoRepoListRecords(ctx context.Context,after string,before string,collection string,limit int,reverse *bool,user string) (*comatprototypes.RepoListRecords_Output, error)
	out, handleErr = s.handleComAtprotoRepoListRecords(ctx, after, before, collection, limit, reverse, user)
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

func (s *Server) HandleComAtprotoServerGetAccountsConfig(c echo.Context) error {
	ctx, span := otel.Tracer("server").Start(c.Request().Context(), "HandleComAtprotoServerGetAccountsConfig")
	defer span.End()
	var out *comatprototypes.ServerGetAccountsConfig_Output
	var handleErr error
	// func (s *Server) handleComAtprotoServerGetAccountsConfig(ctx context.Context) (*comatprototypes.ServerGetAccountsConfig_Output, error)
	out, handleErr = s.handleComAtprotoServerGetAccountsConfig(ctx)
	if handleErr != nil {
		return handleErr
	}
	return c.JSON(200, out)
}

func (s *Server) HandleComAtprotoSessionCreate(c echo.Context) error {
	ctx, span := otel.Tracer("server").Start(c.Request().Context(), "HandleComAtprotoSessionCreate")
	defer span.End()

	var body comatprototypes.SessionCreate_Input
	if err := c.Bind(&body); err != nil {
		return err
	}
	var out *comatprototypes.SessionCreate_Output
	var handleErr error
	// func (s *Server) handleComAtprotoSessionCreate(ctx context.Context,body *comatprototypes.SessionCreate_Input) (*comatprototypes.SessionCreate_Output, error)
	out, handleErr = s.handleComAtprotoSessionCreate(ctx, &body)
	if handleErr != nil {
		return handleErr
	}
	return c.JSON(200, out)
}

func (s *Server) HandleComAtprotoSessionDelete(c echo.Context) error {
	ctx, span := otel.Tracer("server").Start(c.Request().Context(), "HandleComAtprotoSessionDelete")
	defer span.End()
	var handleErr error
	// func (s *Server) handleComAtprotoSessionDelete(ctx context.Context) error
	handleErr = s.handleComAtprotoSessionDelete(ctx)
	if handleErr != nil {
		return handleErr
	}
	return nil
}

func (s *Server) HandleComAtprotoSessionGet(c echo.Context) error {
	ctx, span := otel.Tracer("server").Start(c.Request().Context(), "HandleComAtprotoSessionGet")
	defer span.End()
	var out *comatprototypes.SessionGet_Output
	var handleErr error
	// func (s *Server) handleComAtprotoSessionGet(ctx context.Context) (*comatprototypes.SessionGet_Output, error)
	out, handleErr = s.handleComAtprotoSessionGet(ctx)
	if handleErr != nil {
		return handleErr
	}
	return c.JSON(200, out)
}

func (s *Server) HandleComAtprotoSessionRefresh(c echo.Context) error {
	ctx, span := otel.Tracer("server").Start(c.Request().Context(), "HandleComAtprotoSessionRefresh")
	defer span.End()
	var out *comatprototypes.SessionRefresh_Output
	var handleErr error
	// func (s *Server) handleComAtprotoSessionRefresh(ctx context.Context) (*comatprototypes.SessionRefresh_Output, error)
	out, handleErr = s.handleComAtprotoSessionRefresh(ctx)
	if handleErr != nil {
		return handleErr
	}
	return c.JSON(200, out)
}

func (s *Server) HandleComAtprotoSyncGetRepo(c echo.Context) error {
	ctx, span := otel.Tracer("server").Start(c.Request().Context(), "HandleComAtprotoSyncGetRepo")
	defer span.End()
	did := c.QueryParam("did")
	from := c.QueryParam("from")
	var out io.Reader
	var handleErr error
	// func (s *Server) handleComAtprotoSyncGetRepo(ctx context.Context,did string,from string) (io.Reader, error)
	out, handleErr = s.handleComAtprotoSyncGetRepo(ctx, did, from)
	if handleErr != nil {
		return handleErr
	}
	return c.Stream(200, "application/octet-stream", out)
}

func (s *Server) HandleComAtprotoSyncGetRoot(c echo.Context) error {
	ctx, span := otel.Tracer("server").Start(c.Request().Context(), "HandleComAtprotoSyncGetRoot")
	defer span.End()
	did := c.QueryParam("did")
	var out *comatprototypes.SyncGetRoot_Output
	var handleErr error
	// func (s *Server) handleComAtprotoSyncGetRoot(ctx context.Context,did string) (*comatprototypes.SyncGetRoot_Output, error)
	out, handleErr = s.handleComAtprotoSyncGetRoot(ctx, did)
	if handleErr != nil {
		return handleErr
	}
	return c.JSON(200, out)
}

func (s *Server) HandleComAtprotoSyncUpdateRepo(c echo.Context) error {
	ctx, span := otel.Tracer("server").Start(c.Request().Context(), "HandleComAtprotoSyncUpdateRepo")
	defer span.End()
	body := c.Request().Body
	var handleErr error
	// func (s *Server) handleComAtprotoSyncUpdateRepo(ctx context.Context,r io.Reader) error
	handleErr = s.handleComAtprotoSyncUpdateRepo(ctx, body)
	if handleErr != nil {
		return handleErr
	}
	return nil
}

func (s *Server) HandleComAtprotoPeeringFollow(c echo.Context) error {
	ctx, span := otel.Tracer("server").Start(c.Request().Context(), "HandleComAtprotoPeeringFollow")
	defer span.End()

	var body comatprototypes.PeeringFollow_Input
	if err := c.Bind(&body); err != nil {
		return err
	}
	var handleErr error
	// func (s *Server) handleComAtprotoPeeringFollow(ctx context.Context,body *comatprototypes.PeeringFollow_Input) error
	handleErr = s.handleComAtprotoPeeringFollow(ctx, &body)
	if handleErr != nil {
		return handleErr
	}
	return nil
}

func (s *Server) HandleComAtprotoPeeringInit(c echo.Context) error {
	ctx, span := otel.Tracer("server").Start(c.Request().Context(), "HandleComAtprotoPeeringInit")
	defer span.End()

	var body comatprototypes.PeeringInit_Input
	if err := c.Bind(&body); err != nil {
		return err
	}
	var handleErr error
	// func (s *Server) handleComAtprotoPeeringInit(ctx context.Context,body *comatprototypes.PeeringInit_Input) error
	handleErr = s.handleComAtprotoPeeringInit(ctx, &body)
	if handleErr != nil {
		return handleErr
	}
	return nil
}

func (s *Server) HandleComAtprotoPeeringList(c echo.Context) error {
	ctx, span := otel.Tracer("server").Start(c.Request().Context(), "HandleComAtprotoPeeringList")
	defer span.End()
	var out *comatprototypes.PeeringList_Output
	var handleErr error
	// func (s *Server) handleComAtprotoPeeringList(ctx context.Context) (*comatprototypes.PeeringList_Output, error)
	out, handleErr = s.handleComAtprotoPeeringList(ctx)
	if handleErr != nil {
		return handleErr
	}
	return c.JSON(200, out)
}

func (s *Server) HandleComAtprotoPeeringPropose(c echo.Context) error {
	ctx, span := otel.Tracer("server").Start(c.Request().Context(), "HandleComAtprotoPeeringPropose")
	defer span.End()

	var body comatprototypes.PeeringPropose_Input
	if err := c.Bind(&body); err != nil {
		return err
	}
	var out *comatprototypes.PeeringPropose_Output
	var handleErr error
	// func (s *Server) handleComAtprotoPeeringPropose(ctx context.Context,body *comatprototypes.PeeringPropose_Input) (*comatpototypes.PeeringPropose_Output, error)
	out, handleErr = s.handleComAtprotoPeeringPropose(ctx, &body)
	if handleErr != nil {
		return handleErr
	}
	return c.JSON(200, out)
}
