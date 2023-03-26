package pds

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"time"

	comatprototypes "github.com/bluesky-social/indigo/api/atproto"
	appbskytypes "github.com/bluesky-social/indigo/api/bsky"
	lexutil "github.com/bluesky-social/indigo/lex/util"
	"github.com/ipfs/go-cid"
	"github.com/lestrrat-go/jwx/v2/jwt"
)

func (s *Server) handleAppBskyActorGetProfile(ctx context.Context, actor string) (*appbskytypes.ActorDefs_ProfileViewDetailed, error) {
	profile, err := s.feedgen.GetActorProfile(ctx, actor)
	if err != nil {
		return nil, err
	}

	return &appbskytypes.ActorDefs_ProfileViewDetailed{
		Viewer:         nil, //*ActorGetProfile_MyState `json:"myState" cborgen:"myState"`
		Did:            profile.Did,
		Description:    nil,
		PostsCount:     &profile.Posts,
		FollowsCount:   &profile.Following,
		Handle:         profile.Handle,
		DisplayName:    &profile.DisplayName,
		FollowersCount: &profile.Followers,
	}, nil
}

func (s *Server) handleAppBskyActorGetSuggestions(ctx context.Context, cursor string, limit int) (*appbskytypes.ActorGetSuggestions_Output, error) {

	var out appbskytypes.ActorGetSuggestions_Output
	out.Actors = []*appbskytypes.ActorDefs_ProfileView{}
	return &out, nil
}

func (s *Server) handleAppBskyFeedGetAuthorFeed(ctx context.Context, author string, before string, limit int) (*appbskytypes.FeedGetAuthorFeed_Output, error) {
	_, err := s.getUser(ctx)
	if err != nil {
		return nil, err
	}

	target, err := s.lookupUser(ctx, author)
	//target, err := s.lookupUserByHandle(ctx, author)
	if err != nil {
		return nil, err
	}

	feed, err := s.feedgen.GetAuthorFeed(ctx, target, before, limit)
	if err != nil {
		return nil, err
	}

	out := appbskytypes.FeedGetAuthorFeed_Output{
		Feed: feed,
	}

	return &out, nil
}

func (s *Server) handleAppBskyFeedGetPostThread(ctx context.Context, depth *int, uri string) (*appbskytypes.FeedGetPostThread_Output, error) {
	u, err := s.getUser(ctx)
	if err != nil {
		return nil, err
	}

	d := 6
	if depth != nil {
		d = *depth
	}

	pthread, err := s.feedgen.GetPostThread(ctx, uri, d)
	if err != nil {
		return nil, err
	}

	fmt.Println("TODO: replies")

	var convertToOutputType func(thr *ThreadPost) (*appbskytypes.FeedDefs_ThreadViewPost, error)
	convertToOutputType = func(thr *ThreadPost) (*appbskytypes.FeedDefs_ThreadViewPost, error) {
		p := thr.Post

		vs, err := s.feedgen.getPostViewerState(ctx, thr.PostID, u.ID, u.Did)
		if err != nil {
			return nil, err
		}

		p.Post.Viewer = vs

		out := &appbskytypes.FeedDefs_ThreadViewPost{
			Post: p.Post,
		}

		if thr.ParentUri != "" {
			if thr.Parent == nil {
				out.Parent = &appbskytypes.FeedDefs_ThreadViewPost_Parent{
					FeedDefs_NotFoundPost: &appbskytypes.FeedDefs_NotFoundPost{
						Uri:      thr.ParentUri,
						NotFound: true,
					},
				}
			} else {
				othr, err := convertToOutputType(thr.Parent)
				if err != nil {
					return nil, err
				}

				out.Parent = &appbskytypes.FeedDefs_ThreadViewPost_Parent{
					FeedDefs_ThreadViewPost: othr,
				}
			}
		}

		return out, nil
	}

	othr, err := convertToOutputType(pthread)
	if err != nil {
		return nil, err
	}

	out := appbskytypes.FeedGetPostThread_Output{
		Thread: &appbskytypes.FeedGetPostThread_Output_Thread{
			FeedDefs_ThreadViewPost: othr,
			//FeedGetPostThread_NotFoundPost: &appbskytypes.FeedGetPostThread_NotFoundPost{},
		},
	}

	return &out, nil
}

func (s *Server) handleAppBskyFeedGetRepostedBy(ctx context.Context, before string, cc string, limit int, uri string) (*appbskytypes.FeedGetRepostedBy_Output, error) {
	panic("not yet implemented")
}

func (s *Server) handleAppBskyFeedGetTimeline(ctx context.Context, algorithm string, before string, limit int) (*appbskytypes.FeedGetTimeline_Output, error) {
	u, err := s.getUser(ctx)
	if err != nil {
		return nil, err
	}

	tl, err := s.feedgen.GetTimeline(ctx, u, algorithm, before, limit)
	if err != nil {
		return nil, err
	}

	var out appbskytypes.FeedGetTimeline_Output
	out.Feed = tl

	return &out, nil
}

func (s *Server) handleAppBskyFeedGetLikes(ctx context.Context, cc string, cursor string, limit int, uri string) (*appbskytypes.FeedGetLikes_Output, error) {
	// func (s *Server) handleAppBskyFeedGetLikes(ctx context.Context,cid string,cursor string,limit int,uri string) (*appbskytypes.FeedGetLikes_Output, error)
	pcid, err := cid.Decode(cc)
	if err != nil {
		return nil, err
	}

	votes, err := s.feedgen.GetVotes(ctx, uri, pcid, limit, cursor)
	if err != nil {
		return nil, err
	}

	var out appbskytypes.FeedGetLikes_Output
	out.Uri = uri
	out.Likes = []*appbskytypes.FeedGetLikes_Like{}

	for _, v := range votes {
		out.Likes = append(out.Likes, &appbskytypes.FeedGetLikes_Like{
			Actor:     s.actorBasicToView(ctx, v.Actor),
			IndexedAt: v.IndexedAt.Format(time.RFC3339),
			CreatedAt: v.CreatedAt,
		})
	}

	return &out, nil
}

func (s *Server) handleAppBskyGraphGetFollowers(ctx context.Context, actor string, cursor string, limit int) (*appbskytypes.GraphGetFollowers_Output, error) {
	panic("not yet implemented")
}

func (s *Server) handleAppBskyGraphGetFollows(ctx context.Context, actor string, cursor string, limit int) (*appbskytypes.GraphGetFollows_Output, error) {
	follows, err := s.feedgen.GetFollows(ctx, actor, limit, cursor)
	if err != nil {
		return nil, err
	}

	ai, err := s.feedgen.GetActorProfile(ctx, actor)
	if err != nil {
		return nil, err
	}

	var out appbskytypes.GraphGetFollows_Output
	out.Subject = s.actorBasicToView(ctx, ai.ActorRef())

	out.Follows = []*appbskytypes.ActorDefs_ProfileView{}
	for _, f := range follows {
		out.Follows = append(out.Follows, &appbskytypes.ActorDefs_ProfileView{
			Handle:      f.Subject.Handle,
			DisplayName: f.Subject.DisplayName,
			Did:         f.Subject.Did,
		})
	}

	return &out, nil
}

func (s *Server) handleAppBskyGraphGetMutes(ctx context.Context, before string, limit int) (*appbskytypes.GraphGetMutes_Output, error) {
	panic("not yet implemented")
}

func (s *Server) handleAppBskyGraphMuteActor(ctx context.Context, input *appbskytypes.GraphMuteActor_Input) error {
	panic("not yet implemented")
}

func (s *Server) handleAppBskyGraphUnmuteActor(ctx context.Context, input *appbskytypes.GraphUnmuteActor_Input) error {
	panic("not yet implemented")
}

func (s *Server) handleAppBskyNotificationGetUnreadCount(ctx context.Context) (*appbskytypes.NotificationGetUnreadCount_Output, error) {
	u, err := s.getUser(ctx)
	if err != nil {
		return nil, err
	}

	count, err := s.notifman.GetCount(ctx, u.ID)
	if err != nil {
		return nil, fmt.Errorf("notification getCount: %w", err)
	}

	fmt.Println("notif count: ", u.Handle, count)
	return &appbskytypes.NotificationGetUnreadCount_Output{
		Count: count,
	}, nil
}

func (s *Server) handleAppBskyNotificationListNotifications(ctx context.Context, cursor string, limit int) (*appbskytypes.NotificationListNotifications_Output, error) {
	u, err := s.getUser(ctx)
	if err != nil {
		return nil, err
	}

	notifs, err := s.notifman.GetNotifications(ctx, u.ID)
	if err != nil {
		return nil, err
	}

	return &appbskytypes.NotificationListNotifications_Output{
		Notifications: notifs,
	}, nil
}

func (s *Server) handleAppBskyNotificationUpdateSeen(ctx context.Context, input *appbskytypes.NotificationUpdateSeen_Input) error {
	u, err := s.getUser(ctx)
	if err != nil {
		return err
	}

	seen, err := time.Parse(time.RFC3339, input.SeenAt)
	if err != nil {
		return fmt.Errorf("invalid time format for 'seenAt': %w", err)
	}

	return s.notifman.UpdateSeen(ctx, u.ID, seen)
}

func (s *Server) handleComAtprotoServerCreateAccount(ctx context.Context, body *comatprototypes.ServerCreateAccount_Input) (*comatprototypes.ServerCreateAccount_Output, error) {

	if err := validateEmail(body.Email); err != nil {
		return nil, err
	}

	if err := s.validateHandle(body.Handle); err != nil {
		return nil, err
	}

	_, err := s.lookupUserByHandle(ctx, body.Handle)
	switch err {
	default:
		return nil, err
	case nil:
		return nil, fmt.Errorf("handle already registered")
	case ErrNoSuchUser:
		// handle is available, lets go
	}

	var recoveryKey string
	if body.RecoveryKey != nil {
		recoveryKey = *body.RecoveryKey
	}

	u := User{
		Handle:      body.Handle,
		Password:    body.Password,
		RecoveryKey: recoveryKey,
		Email:       body.Email,
	}
	if err := s.db.Create(&u).Error; err != nil {
		return nil, err
	}

	if recoveryKey == "" {
		recoveryKey = s.signingKey.Public().DID()
	}

	d, err := s.plc.CreateDID(ctx, s.signingKey, recoveryKey, body.Handle, s.serviceUrl)
	if err != nil {
		return nil, fmt.Errorf("create did: %w", err)
	}

	u.Did = d
	if err := s.db.Save(&u).Error; err != nil {
		return nil, err
	}

	if err := s.repoman.InitNewActor(ctx, u.ID, u.Handle, u.Did, "", UserActorDeclCid, UserActorDeclType); err != nil {
		return nil, err
	}

	tok, err := s.createAuthTokenForUser(ctx, body.Handle, d)
	if err != nil {
		return nil, err
	}

	return &comatprototypes.ServerCreateAccount_Output{
		Handle:     body.Handle,
		Did:        d,
		AccessJwt:  tok.AccessJwt,
		RefreshJwt: tok.RefreshJwt,
	}, nil
}

func (s *Server) handleComAtprotoServerCreateInviteCode(ctx context.Context, body *comatprototypes.ServerCreateInviteCode_Input) (*comatprototypes.ServerCreateInviteCode_Output, error) {
	u, err := s.getUser(ctx)
	if err != nil {
		return nil, err
	}

	_ = u

	return nil, fmt.Errorf("invite codes not currently supported")
}

func (s *Server) handleComAtprotoServerRequestAccountDelete(ctx context.Context) error {
	panic("not yet implemented")
}

func (s *Server) handleComAtprotoServerDeleteAccount(ctx context.Context, body *comatprototypes.ServerDeleteAccount_Input) error {
	panic("not yet implemented")
}

func (s *Server) handleComAtprotoServerRequestPasswordReset(ctx context.Context, body *comatprototypes.ServerRequestPasswordReset_Input) error {
	panic("not yet implemented")
}

func (s *Server) handleComAtprotoServerResetPassword(ctx context.Context, body *comatprototypes.ServerResetPassword_Input) error {
	panic("not yet implemented")
}

func (s *Server) handleComAtprotoRepoUploadBlob(ctx context.Context, r io.Reader, contentType string) (*comatprototypes.RepoUploadBlob_Output, error) {
	panic("not yet implemented")
}

func (s *Server) handleComAtprotoIdentityResolveHandle(ctx context.Context, handle string) (*comatprototypes.IdentityResolveHandle_Output, error) {
	if handle == "" {
		return &comatprototypes.IdentityResolveHandle_Output{Did: s.signingKey.Public().DID()}, nil
	}
	u, err := s.lookupUserByHandle(ctx, handle)
	if err != nil {
		return nil, err
	}

	return &comatprototypes.IdentityResolveHandle_Output{Did: u.Did}, nil
}

func (s *Server) handleComAtprotoRepoApplyWrites(ctx context.Context, body *comatprototypes.RepoApplyWrites_Input) error {
	u, err := s.getUser(ctx)
	if err != nil {
		return err
	}

	if u.Did != body.Repo {
		return fmt.Errorf("writes for non-user actors not supported (DID mismatch)")
	}

	return s.repoman.BatchWrite(ctx, u.ID, body.Writes)
}

func (s *Server) handleComAtprotoRepoCreateRecord(ctx context.Context, input *comatprototypes.RepoCreateRecord_Input) (*comatprototypes.RepoCreateRecord_Output, error) {
	u, err := s.getUser(ctx)
	if err != nil {
		return nil, fmt.Errorf("get user: %w", err)
	}

	rpath, recid, err := s.repoman.CreateRecord(ctx, u.ID, input.Collection, input.Record.Val)
	if err != nil {
		return nil, fmt.Errorf("record create: %w", err)
	}

	return &comatprototypes.RepoCreateRecord_Output{
		Uri: "at://" + u.Did + "/" + rpath,
		Cid: recid.String(),
	}, nil
}

func (s *Server) handleComAtprotoRepoDeleteRecord(ctx context.Context, input *comatprototypes.RepoDeleteRecord_Input) error {
	u, err := s.getUser(ctx)
	if err != nil {
		return err
	}

	if u.Did != input.Repo {
		return fmt.Errorf("specified DID did not match authed user")
	}

	return s.repoman.DeleteRecord(ctx, u.ID, input.Collection, input.Rkey)
}

func (s *Server) handleComAtprotoRepoGetRecord(ctx context.Context, c string, collection string, repo string, rkey string) (*comatprototypes.RepoGetRecord_Output, error) {
	targetUser, err := s.lookupUser(ctx, repo)
	if err != nil {
		return nil, err
	}

	var maybeCid cid.Cid
	if c != "" {
		cc, err := cid.Decode(c)
		if err != nil {
			return nil, err
		}
		maybeCid = cc
	}

	reccid, rec, err := s.repoman.GetRecord(ctx, targetUser.ID, collection, rkey, maybeCid)
	if err != nil {
		return nil, fmt.Errorf("repoman GetRecord: %w", err)
	}

	ccstr := reccid.String()
	return &comatprototypes.RepoGetRecord_Output{
		Cid:   &ccstr,
		Uri:   "at://" + targetUser.Did + "/" + collection + "/" + rkey,
		Value: &lexutil.LexiconTypeDecoder{rec},
	}, nil
}

func (s *Server) handleComAtprotoRepoListRecords(ctx context.Context, collection string, limit int, repo string, reverse *bool, rkeyEnd string, rkeyStart string) (*comatprototypes.RepoListRecords_Output, error) {
	panic("not yet implemented")
}

func (s *Server) handleComAtprotoRepoPutRecord(ctx context.Context, input *comatprototypes.RepoPutRecord_Input) (*comatprototypes.RepoPutRecord_Output, error) {
	panic("not yet implemented")
}

func (s *Server) handleComAtprotoServerDescribeServer(ctx context.Context) (*comatprototypes.ServerDescribeServer_Output, error) {
	invcode := false
	return &comatprototypes.ServerDescribeServer_Output{
		InviteCodeRequired: &invcode,
		AvailableUserDomains: []string{
			s.handleSuffix,
		},
		Links: &comatprototypes.ServerDescribeServer_Links{},
	}, nil
}

var ErrInvalidUsernameOrPassword = fmt.Errorf("invalid username or password")

func (s *Server) handleComAtprotoServerCreateSession(ctx context.Context, body *comatprototypes.ServerCreateSession_Input) (*comatprototypes.ServerCreateSession_Output, error) {
	u, err := s.lookupUserByHandle(ctx, *body.Identifier)
	if err != nil {
		return nil, err
	}

	if body.Password != u.Password {
		return nil, ErrInvalidUsernameOrPassword
	}

	tok, err := s.createAuthTokenForUser(ctx, *body.Identifier, u.Did)
	if err != nil {
		return nil, err
	}

	return &comatprototypes.ServerCreateSession_Output{
		Handle:     *body.Identifier,
		Did:        u.Did,
		AccessJwt:  tok.AccessJwt,
		RefreshJwt: tok.RefreshJwt,
	}, nil
}

func (s *Server) handleComAtprotoServerDeleteSession(ctx context.Context) error {
	panic("not yet implemented")
}

func (s *Server) handleComAtprotoServerGetSession(ctx context.Context) (*comatprototypes.ServerGetSession_Output, error) {
	u, err := s.getUser(ctx)
	if err != nil {
		return nil, err
	}

	return &comatprototypes.ServerGetSession_Output{
		Handle: u.Handle,
		Did:    u.Did,
	}, nil
}

func (s *Server) handleComAtprotoServerRefreshSession(ctx context.Context) (*comatprototypes.ServerRefreshSession_Output, error) {
	u, err := s.getUser(ctx)
	if err != nil {
		return nil, err
	}

	scope, ok := ctx.Value("authScope").(string)
	if !ok {
		return nil, fmt.Errorf("scope not present in refresh token")
	}

	if scope != "com.atproto.refresh" {
		return nil, fmt.Errorf("auth token did not have refresh scope")
	}

	tok, ok := ctx.Value("token").(*jwt.Token)
	if !ok {
		return nil, fmt.Errorf("internal auth error: token not set post auth check")
	}

	if err := s.invalidateToken(ctx, u, tok); err != nil {
		return nil, err
	}

	outTok, err := s.createAuthTokenForUser(ctx, u.Handle, u.Did)
	if err != nil {
		return nil, err
	}

	return &comatprototypes.ServerRefreshSession_Output{
		Handle:     u.Handle,
		Did:        u.Did,
		AccessJwt:  outTok.AccessJwt,
		RefreshJwt: outTok.RefreshJwt,
	}, nil

}

func (s *Server) handleComAtprotoSyncUpdateRepo(ctx context.Context, r io.Reader) error {
	panic("not yet implemented")
}

func (s *Server) handleComAtprotoSyncGetCheckout(ctx context.Context, commit string, did string) (io.Reader, error) {
	panic("not yet implemented")
}

func (s *Server) handleComAtprotoSyncGetCommitPath(ctx context.Context, did string, earliest string, latest string) (*comatprototypes.SyncGetCommitPath_Output, error) {
	panic("not yet implemented")
}

func (s *Server) handleComAtprotoSyncGetHead(ctx context.Context, did string) (*comatprototypes.SyncGetHead_Output, error) {
	user, err := s.lookupUserByDid(ctx, did)
	if err != nil {
		return nil, err
	}

	root, err := s.repoman.GetRepoRoot(ctx, user.ID)
	if err != nil {
		return nil, err
	}

	return &comatprototypes.SyncGetHead_Output{
		Root: root.String(),
	}, nil
}

func (s *Server) handleComAtprotoSyncGetRecord(ctx context.Context, collection string, commit string, did string, rkey string) (io.Reader, error) {
	panic("not yet implemented")
}

func (s *Server) handleComAtprotoSyncGetRepo(ctx context.Context, did string, earliest, latest string) (io.Reader, error) {
	var earlyCid cid.Cid
	if earliest != "" {
		cc, err := cid.Decode(earliest)
		if err != nil {
			return nil, err
		}

		earlyCid = cc
	}

	var lateCid cid.Cid
	if latest != "" {
		cc, err := cid.Decode(latest)
		if err != nil {
			return nil, err
		}

		lateCid = cc
	}

	targetUser, err := s.lookupUser(ctx, did)
	if err != nil {
		return nil, err
	}

	buf := new(bytes.Buffer)
	if err := s.repoman.ReadRepo(ctx, targetUser.ID, earlyCid, lateCid, buf); err != nil {
		return nil, err
	}

	return buf, nil
}

func (s *Server) handleAppBskyActorGetProfiles(ctx context.Context, actors []string) (*appbskytypes.ActorGetProfiles_Output, error) {
	panic("nyi")
}

func (s *Server) handleComAtprotoAdminGetModerationAction(ctx context.Context, id int) (*comatprototypes.AdminDefs_ActionViewDetail, error) {
	panic("nyi")
}
func (s *Server) handleComAtprotoAdminGetModerationActions(ctx context.Context, before string, limit int, subject string) (*comatprototypes.AdminGetModerationActions_Output, error) {
	panic("nyi")
}
func (s *Server) handleComAtprotoAdminGetModerationReport(ctx context.Context, id int) (*comatprototypes.AdminDefs_ReportViewDetail, error) {
	panic("nyi")
}
func (s *Server) handleComAtprotoAdminGetModerationReports(ctx context.Context, before string, limit int, resolved *bool, subject string) (*comatprototypes.AdminGetModerationReports_Output, error) {
	panic("nyi")
}

func (s *Server) handleComAtprotoAdminGetRecord(ctx context.Context, cid string, uri string) (*comatprototypes.AdminDefs_RecordViewDetail, error) {
	panic("nyi")
}

func (s *Server) handleComAtprotoAdminGetRepo(ctx context.Context, did string) (*comatprototypes.AdminDefs_RepoViewDetail, error) {
	panic("nyi")
}
func (s *Server) handleComAtprotoAdminResolveModerationReports(ctx context.Context, body *comatprototypes.AdminResolveModerationReports_Input) (*comatprototypes.AdminDefs_ActionView, error) {
	panic("nyi")
}
func (s *Server) handleComAtprotoAdminReverseModerationAction(ctx context.Context, body *comatprototypes.AdminReverseModerationAction_Input) (*comatprototypes.AdminDefs_ActionView, error) {
	panic("nyi")
}
func (s *Server) handleComAtprotoAdminSearchRepos(ctx context.Context, before string, limit int, term string) (*comatprototypes.AdminSearchRepos_Output, error) {
	panic("nyi")
}
func (s *Server) handleComAtprotoAdminTakeModerationAction(ctx context.Context, body *comatprototypes.AdminTakeModerationAction_Input) (*comatprototypes.AdminDefs_ActionView, error) {
	panic("nyi")
}

func (s *Server) handleComAtprotoSyncGetBlocks(ctx context.Context, cids []string, did string) (io.Reader, error) {
	panic("nyi")
}

func (s *Server) handleComAtprotoSyncNotifyOfUpdate(ctx context.Context, hostname string) error {
	panic("nyi")
}

func (s *Server) handleComAtprotoSyncRequestCrawl(ctx context.Context, host string) error {
	panic("nyi")
}

func (s *Server) handleComAtprotoSyncGetBlob(ctx context.Context, cid string, did string) (io.Reader, error) {
	panic("nyi")
}

func (s *Server) handleComAtprotoSyncListBlobs(ctx context.Context, did string, earliest string, latest string) (*comatprototypes.SyncListBlobs_Output, error) {
	panic("nyi")
}

func (s *Server) handleAppBskyActorSearchActors(ctx context.Context, cursor string, limit int, term string) (*appbskytypes.ActorSearchActors_Output, error) {
	panic("nyi")
}

func (s *Server) handleAppBskyActorSearchActorsTypeahead(ctx context.Context, limit int, term string) (*appbskytypes.ActorSearchActorsTypeahead_Output, error) {
	panic("nyi")
}

func (s *Server) handleAppBskyUnspeccedGetPopular(ctx context.Context, cursor string, limit int) (*appbskytypes.UnspeccedGetPopular_Output, error) {
	panic("nyi")
}

func (s *Server) handleComAtprotoIdentityUpdateHandle(ctx context.Context, body *comatprototypes.IdentityUpdateHandle_Input) error {
	panic("nyi")
}

func (s *Server) handleComAtprotoModerationCreateReport(ctx context.Context, body *comatprototypes.ModerationCreateReport_Input) (*comatprototypes.ModerationCreateReport_Output, error) {
	panic("nyi")
}

func (s *Server) handleComAtprotoRepoDescribeRepo(ctx context.Context, repo string) (*comatprototypes.RepoDescribeRepo_Output, error) {
	panic("nyi")
}
