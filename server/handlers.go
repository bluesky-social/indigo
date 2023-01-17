package schemagen

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/ipfs/go-cid"
	jwt "github.com/lestrrat-go/jwx/jwt"
	comatprototypes "github.com/whyrusleeping/gosky/api/atproto"
	appbskytypes "github.com/whyrusleeping/gosky/api/bsky"
	"github.com/whyrusleeping/gosky/lex/util"
)

func (s *Server) handleAppBskyActorCreateScene(ctx context.Context, input *appbskytypes.ActorCreateScene_Input) (*appbskytypes.ActorCreateScene_Output, error) {
	u, err := s.getUser(ctx)
	if err != nil {
		return nil, err
	}

	_ = u
	panic("nyi")

	/*
		return &appbskytypes.ActorCreateScene_Output{
			Declaration: scinfo.Declaration,
			Did:         scinfo.Did,
			Handle:      scinfo.Handle,
		}, nil
	*/
}

func (s *Server) handleAppBskyActorGetProfile(ctx context.Context, actor string) (*appbskytypes.ActorGetProfile_Output, error) {
	profile, err := s.feedgen.GetActorProfile(ctx, actor)
	if err != nil {
		return nil, err
	}

	return &appbskytypes.ActorGetProfile_Output{
		MyState: nil, //*ActorGetProfile_MyState `json:"myState" cborgen:"myState"`
		Did:     profile.Did,
		Declaration: &appbskytypes.SystemDeclRef{
			Cid:       profile.DeclRefCid,
			ActorType: profile.Type,
		},
		Description:    nil,
		PostsCount:     profile.Posts,
		FollowsCount:   profile.Following,
		MembersCount:   0, // TODO:
		Handle:         profile.Handle,
		Creator:        "", //TODO:
		DisplayName:    &profile.DisplayName,
		FollowersCount: profile.Followers,
	}, nil
}

func (s *Server) handleAppBskyActorGetSuggestions(ctx context.Context, cursor string, limit int) (*appbskytypes.ActorGetSuggestions_Output, error) {

	var out appbskytypes.ActorGetSuggestions_Output
	out.Actors = []*appbskytypes.ActorGetSuggestions_Actor{}
	return &out, nil
}

func (s *Server) handleAppBskyActorSearch(ctx context.Context, before string, limit int, term string) (*appbskytypes.ActorSearch_Output, error) {
	panic("not yet implemented")
}

func (s *Server) handleAppBskyActorSearchTypeahead(ctx context.Context, limit int, term string) (*appbskytypes.ActorSearchTypeahead_Output, error) {
	panic("not yet implemented")
}

func (s *Server) handleAppBskyActorUpdateProfile(ctx context.Context, input *appbskytypes.ActorUpdateProfile_Input) (*appbskytypes.ActorUpdateProfile_Output, error) {
	u, err := s.getUser(ctx)
	if err != nil {
		return nil, err
	}

	profile, err := s.repoman.GetProfile(ctx, u.ID)
	if err != nil {
		return nil, err
	}

	if input.DisplayName != nil {
		profile.DisplayName = *input.DisplayName
	}

	if input.DisplayName != nil {
		profile.Description = input.Description
	}

	if input.Avatar != nil {
		profile.Avatar = input.Avatar
	}

	if input.Banner != nil {
		profile.Banner = input.Banner
	}

	ncid, err := s.repoman.UpdateRecord(ctx, u.ID, "app.bsky.actor.profile", "self", profile)
	if err != nil {
		return nil, err
	}

	return &appbskytypes.ActorUpdateProfile_Output{
		Cid:    ncid.String(),
		Uri:    "at://" + u.Did + "/app.bsky.actor.profile/self",
		Record: util.LexiconTypeDecoder{profile},
	}, nil
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

	var convertToOutputType func(thr *ThreadPost) (*appbskytypes.FeedGetPostThread_ThreadViewPost, error)
	convertToOutputType = func(thr *ThreadPost) (*appbskytypes.FeedGetPostThread_ThreadViewPost, error) {
		p := thr.Post

		vs, err := s.feedgen.getPostViewerState(ctx, thr.PostID, u.ID, u.Did)
		if err != nil {
			return nil, err
		}

		p.Post.Viewer = vs

		out := &appbskytypes.FeedGetPostThread_ThreadViewPost{
			Post: p.Post,
		}

		if thr.ParentUri != "" {
			if thr.Parent == nil {
				out.Parent = &appbskytypes.FeedGetPostThread_ThreadViewPost_Parent{
					FeedGetPostThread_NotFoundPost: &appbskytypes.FeedGetPostThread_NotFoundPost{
						Uri:      thr.ParentUri,
						NotFound: true,
					},
				}
			} else {
				othr, err := convertToOutputType(thr.Parent)
				if err != nil {
					return nil, err
				}

				out.Parent = &appbskytypes.FeedGetPostThread_ThreadViewPost_Parent{
					FeedGetPostThread_ThreadViewPost: othr,
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
			FeedGetPostThread_ThreadViewPost: othr,
			//FeedGetPostThread_NotFoundPost: &appbskytypes.FeedGetPostThread_NotFoundPost{},
		},
	}

	return &out, nil
}

func (s *Server) handleAppBskyFeedGetRepostedBy(ctx context.Context, before string, cid string, limit int, uri string) (*appbskytypes.FeedGetRepostedBy_Output, error) {
	panic("not yet implemented")
	//appbskytypes.FeedGetRepostedBy_Output{}
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

func (s *Server) handleAppBskyFeedGetVotes(ctx context.Context, before string, cc string, direction string, limit int, uri string) (*appbskytypes.FeedGetVotes_Output, error) {
	pcid, err := cid.Decode(cc)
	if err != nil {
		return nil, err
	}

	votes, err := s.feedgen.GetVotes(ctx, uri, pcid, direction, limit, before)
	if err != nil {
		return nil, err
	}

	var out appbskytypes.FeedGetVotes_Output
	out.Uri = uri
	out.Votes = []*appbskytypes.FeedGetVotes_Vote{}

	for _, v := range votes {
		out.Votes = append(out.Votes, &appbskytypes.FeedGetVotes_Vote{
			Actor:     v.Actor,
			Direction: v.Direction,
			IndexedAt: v.IndexedAt.Format(time.RFC3339),
			CreatedAt: v.CreatedAt,
		})
	}

	return &out, nil
}

func (s *Server) handleAppBskyFeedSetVote(ctx context.Context, input *appbskytypes.FeedSetVote_Input) (*appbskytypes.FeedSetVote_Output, error) {
	u, err := s.getUser(ctx)
	if err != nil {
		return nil, err
	}

	// TODO: check subject actually exists maybe?
	vote := &appbskytypes.FeedVote{
		Direction: input.Direction,
		CreatedAt: time.Now().Format(time.RFC3339),
		Subject:   input.Subject,
	}

	rpath, _, err := s.repoman.CreateRecord(ctx, u.ID, "app.bsky.feed.vote", vote)
	if err != nil {
		return nil, err
	}

	uri := "at://" + u.Did + "/" + rpath
	if input.Direction == "up" {
		return &appbskytypes.FeedSetVote_Output{
			Upvote: &uri,
		}, nil
	} else if input.Direction == "down" {
		return &appbskytypes.FeedSetVote_Output{
			Downvote: &uri,
		}, nil
	} else {
		return nil, fmt.Errorf("strange place to catch an invalid vote direction")
	}
}

func (s *Server) handleAppBskyGraphGetAssertions(ctx context.Context, assertion string, author string, before string, confirmed *bool, limit int, subject string) (*appbskytypes.GraphGetAssertions_Output, error) {
	panic("not yet implemented")
}

func (s *Server) handleAppBskyGraphGetFollowers(ctx context.Context, before string, limit int, user string) (*appbskytypes.GraphGetFollowers_Output, error) {
	panic("not yet implemented")
}

func (s *Server) handleAppBskyGraphGetFollows(ctx context.Context, before string, limit int, user string) (*appbskytypes.GraphGetFollows_Output, error) {
	follows, err := s.feedgen.GetFollows(ctx, user, limit, before)
	if err != nil {
		return nil, err
	}

	ai, err := s.feedgen.GetActorProfile(ctx, user)
	if err != nil {
		return nil, err
	}

	var out appbskytypes.GraphGetFollows_Output
	out.Subject = ai.ActorRef()

	out.Follows = []*appbskytypes.GraphGetFollows_Follow{}
	for _, f := range follows {
		out.Follows = append(out.Follows, &appbskytypes.GraphGetFollows_Follow{
			Declaration: f.Subject.Declaration,
			Handle:      f.Subject.Handle,
			DisplayName: f.Subject.DisplayName,
			Did:         f.Subject.Did,
			CreatedAt:   &f.CreatedAt,
			IndexedAt:   f.IndexedAt,
		})
	}

	return &out, nil
}

func (s *Server) handleAppBskyGraphGetMembers(ctx context.Context, actor string, before string, limit int) (*appbskytypes.GraphGetMembers_Output, error) {
	panic("not yet implemented")
}

func (s *Server) handleAppBskyGraphGetMemberships(ctx context.Context, actor string, before string, limit int) (*appbskytypes.GraphGetMemberships_Output, error) {
	ai, err := s.feedgen.GetActorProfile(ctx, actor)
	if err != nil {
		return nil, err
	}

	return &appbskytypes.GraphGetMemberships_Output{
		Subject:     ai.ActorRef(),
		Memberships: []*appbskytypes.GraphGetMemberships_Membership{},
	}, nil
}

func (s *Server) handleAppBskyNotificationGetCount(ctx context.Context) (*appbskytypes.NotificationGetCount_Output, error) {
	u, err := s.getUser(ctx)
	if err != nil {
		return nil, err
	}

	count, err := s.notifman.GetCount(ctx, u.ID)
	if err != nil {
		return nil, fmt.Errorf("notification getCount: %w", err)
	}

	fmt.Println("notif count: ", u.Handle, count)
	return &appbskytypes.NotificationGetCount_Output{
		Count: count,
	}, nil
}

func (s *Server) handleAppBskyNotificationList(ctx context.Context, before string, limit int) (*appbskytypes.NotificationList_Output, error) {
	u, err := s.getUser(ctx)
	if err != nil {
		return nil, err
	}

	notifs, err := s.notifman.GetNotifications(ctx, u.ID)
	if err != nil {
		return nil, err
	}

	return &appbskytypes.NotificationList_Output{
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

func (s *Server) handleComAtprotoAccountCreate(ctx context.Context, input *comatprototypes.AccountCreate_Input) (*comatprototypes.AccountCreate_Output, error) {

	if err := validateEmail(input.Email); err != nil {
		return nil, err
	}

	if err := s.validateHandle(input.Handle); err != nil {
		return nil, err
	}

	_, err := s.lookupUserByHandle(ctx, input.Handle)
	switch err {
	default:
		return nil, err
	case nil:
		return nil, fmt.Errorf("handle already registered")
	case ErrNoSuchUser:
		// handle is available, lets go
	}

	var recoveryKey string
	if input.RecoveryKey != nil {
		recoveryKey = *input.RecoveryKey
	}

	u := User{
		Handle:      input.Handle,
		Password:    input.Password,
		RecoveryKey: recoveryKey,
		Email:       input.Email,
	}
	if err := s.db.Create(&u).Error; err != nil {
		return nil, err
	}

	d, err := s.plc.CreateDID(ctx, s.signingKey, recoveryKey, input.Handle, s.serviceUrl)
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

	tok, err := s.createAuthTokenForUser(ctx, input.Handle, d)
	if err != nil {
		return nil, err
	}

	return &comatprototypes.AccountCreate_Output{
		Handle:     input.Handle,
		Did:        d,
		AccessJwt:  tok.AccessJwt,
		RefreshJwt: tok.RefreshJwt,
	}, nil
}

func (s *Server) handleComAtprotoAccountCreateInviteCode(ctx context.Context, input *comatprototypes.AccountCreateInviteCode_Input) (*comatprototypes.AccountCreateInviteCode_Output, error) {
	u, err := s.getUser(ctx)
	if err != nil {
		return nil, err
	}

	_ = u

	return nil, fmt.Errorf("invite codes not currently supported")
}

func (s *Server) handleComAtprotoAccountDelete(ctx context.Context) error {
	panic("not yet implemented")
}

func (s *Server) handleComAtprotoAccountGet(ctx context.Context) error {
	return nil
}

func (s *Server) handleComAtprotoAccountRequestPasswordReset(ctx context.Context, input *comatprototypes.AccountRequestPasswordReset_Input) error {
	panic("not yet implemented")
}

func (s *Server) handleComAtprotoAccountResetPassword(ctx context.Context, input *comatprototypes.AccountResetPassword_Input) error {
	panic("not yet implemented")
}

func (s *Server) handleComAtprotoHandleResolve(ctx context.Context, handle string) (*comatprototypes.HandleResolve_Output, error) {

	u, err := s.lookupUserByHandle(ctx, handle)
	if err != nil {
		return nil, err
	}

	return &comatprototypes.HandleResolve_Output{Did: u.Did}, nil
}

func (s *Server) handleComAtprotoRepoBatchWrite(ctx context.Context, input *comatprototypes.RepoBatchWrite_Input) error {
	u, err := s.getUser(ctx)
	if err != nil {
		return err
	}

	if u.Did != input.Did {
		return fmt.Errorf("writes for non-user actors not supported (DID mismatch)")
	}

	return s.repoman.BatchWrite(ctx, u.ID, input.Writes)
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

	if u.Did != input.Did {
		return fmt.Errorf("specified DID did not match authed user")
	}

	return s.repoman.DeleteRecord(ctx, u.ID, input.Collection, input.Rkey)
}

func (s *Server) handleComAtprotoRepoDescribe(ctx context.Context, user string) (*comatprototypes.RepoDescribe_Output, error) {
	panic("not yet implemented")
}

func (s *Server) handleComAtprotoRepoGetRecord(ctx context.Context, c string, collection string, rkey string, user string) (*comatprototypes.RepoGetRecord_Output, error) {
	targetUser, err := s.lookupUser(ctx, user)
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
		Value: util.LexiconTypeDecoder{rec},
	}, nil
}

func (s *Server) handleComAtprotoRepoListRecords(ctx context.Context, after string, before string, collection string, limit int, reverse *bool, user string) (*comatprototypes.RepoListRecords_Output, error) {
	panic("not yet implemented")
}

func (s *Server) handleComAtprotoRepoPutRecord(ctx context.Context, input *comatprototypes.RepoPutRecord_Input) (*comatprototypes.RepoPutRecord_Output, error) {
	panic("not yet implemented")
}

func (s *Server) handleComAtprotoServerGetAccountsConfig(ctx context.Context) (*comatprototypes.ServerGetAccountsConfig_Output, error) {
	invcode := false
	return &comatprototypes.ServerGetAccountsConfig_Output{
		InviteCodeRequired: &invcode,
		AvailableUserDomains: []string{
			s.handleSuffix,
		},
		Links: &comatprototypes.ServerGetAccountsConfig_Links{},
	}, nil
}

func (s *Server) handleComAtprotoSessionCreate(ctx context.Context, input *comatprototypes.SessionCreate_Input) (*comatprototypes.SessionCreate_Output, error) {
	u, err := s.lookupUserByHandle(ctx, input.Handle)
	if err != nil {
		return nil, err
	}

	if input.Password != u.Password {
		return nil, fmt.Errorf("invalid username or password")
	}

	tok, err := s.createAuthTokenForUser(ctx, input.Handle, u.Did)
	if err != nil {
		return nil, err
	}

	return &comatprototypes.SessionCreate_Output{
		Handle:     input.Handle,
		Did:        u.Did,
		AccessJwt:  tok.AccessJwt,
		RefreshJwt: tok.RefreshJwt,
	}, nil
}

func (s *Server) handleComAtprotoSessionDelete(ctx context.Context) error {
	panic("not yet implemented")
}

func (s *Server) handleComAtprotoSessionGet(ctx context.Context) (*comatprototypes.SessionGet_Output, error) {
	u, err := s.getUser(ctx)
	if err != nil {
		return nil, err
	}

	return &comatprototypes.SessionGet_Output{
		Handle: u.Handle,
		Did:    u.Did,
	}, nil
}

func (s *Server) handleComAtprotoSessionRefresh(ctx context.Context) (*comatprototypes.SessionRefresh_Output, error) {
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

	return &comatprototypes.SessionRefresh_Output{
		Handle:     u.Handle,
		Did:        u.Did,
		AccessJwt:  outTok.AccessJwt,
		RefreshJwt: outTok.RefreshJwt,
	}, nil

}

func (s *Server) handleComAtprotoSyncGetRepo(ctx context.Context, did string, from string) (io.Reader, error) {
	var fromcid cid.Cid
	if from != "" {
		cc, err := cid.Decode(from)
		if err != nil {
			return nil, err
		}

		fromcid = cc
	}

	targetUser, err := s.lookupUser(ctx, did)
	if err != nil {
		return nil, err
	}

	buf := new(bytes.Buffer)
	if err := s.repoman.ReadRepo(ctx, targetUser.ID, fromcid, buf); err != nil {
		return nil, err
	}

	return buf, nil
}

func (s *Server) handleComAtprotoSyncGetRoot(ctx context.Context, did string) (*comatprototypes.SyncGetRoot_Output, error) {
	user, err := s.lookupUserByDid(ctx, did)
	if err != nil {
		return nil, err
	}

	root, err := s.repoman.GetRepoRoot(ctx, user.ID)
	if err != nil {
		return nil, err
	}

	return &comatprototypes.SyncGetRoot_Output{
		Root: root.String(),
	}, nil
}

func (s *Server) handleComAtprotoSyncUpdateRepo(ctx context.Context, r io.Reader) error {
	panic("not yet implemented")
}

func (s *Server) handleComAtprotoBlobUpload(ctx context.Context, r io.Reader, ctype string) (*comatprototypes.BlobUpload_Output, error) {
	panic("not yet implemented")
}

func (s *Server) handleAppBskyGraphGetMutes(ctx context.Context, before string, limit int) (*appbskytypes.GraphGetMutes_Output, error) {
	panic("not yet implemented")
}

func (s *Server) handleAppBskyGraphMute(ctx context.Context, input *appbskytypes.GraphMute_Input) error {
	panic("not yet implemented")
}

func (s *Server) handleAppBskyGraphUnmute(ctx context.Context, input *appbskytypes.GraphUnmute_Input) error {
	panic("not yet implemented")
}

func (s *Server) handleComAtprotoPeeringInit(ctx context.Context, body *comatprototypes.PeeringInit_Input) error {
	panic("not yet implemented")
}

func (s *Server) handleComAtprotoPeeringPropose(ctx context.Context, body *comatprototypes.PeeringPropose_Input) (*comatprototypes.PeeringPropose_Output, error) {
	panic("not yet implemented")
}

func (s *Server) handleComAtprotoPeeringList(ctx context.Context) (*comatprototypes.PeeringList_Output, error) {
	panic("not yet implemented")
}

func (s *Server) handleComAtprotoPeeringFollow(ctx context.Context, body *comatprototypes.PeeringFollow_Input) error {
	// TODO: cross server auth checks
	auth, ok := ctx.Value("auth").(string)
	if !ok {
		return fmt.Errorf("no auth present in peering.follow request header")
	}

	auth = strings.TrimPrefix(auth, "Bearer ")
	tok, err := jwt.ParseString(auth)
	if err != nil {
		return err
	}

	v, ok := tok.Get("pds")
	if !ok {
		panic("im a bad programmer")
	}

	opdsdid := v.(string)
	//

	for _, u := range body.Users {
		if err := s.AddRemoteFollow(ctx, opdsdid, u); err != nil {
			return fmt.Errorf("handle add remote follow: %w", err)
		}
	}

	return nil
}
