package schemagen

import (
	"context"
	"io"

	comatprototypes "github.com/whyrusleeping/gosky/api/atproto"
	appbskytypes "github.com/whyrusleeping/gosky/api/bsky"
)

func (s *Server) handleAppBskyActorCreateScene(ctx context.Context, input *appbskytypes.ActorCreateScene_Input) (*appbskytypes.ActorCreateScene_Output, error) {
	panic("not yet implemented")
}

func (s *Server) handleAppBskyActorGetProfile(ctx context.Context, actor string) (*appbskytypes.ActorGetProfile_Output, error) {
	panic("not yet implemented")
}

func (s *Server) handleAppBskyActorGetSuggestions(ctx context.Context, cursor string, limit int) (*appbskytypes.ActorGetSuggestions_Output, error) {
	panic("not yet implemented")
}

func (s *Server) handleAppBskyActorSearch(ctx context.Context, before string, limit int, term string) (*appbskytypes.ActorSearch_Output, error) {
	panic("not yet implemented")
}

func (s *Server) handleAppBskyActorSearchTypeahead(ctx context.Context, limit int, term string) (*appbskytypes.ActorSearchTypeahead_Output, error) {
	panic("not yet implemented")
}

func (s *Server) handleAppBskyActorUpdateProfile(ctx context.Context, input *appbskytypes.ActorUpdateProfile_Input) (*appbskytypes.ActorUpdateProfile_Output, error) {
	panic("not yet implemented")
}

func (s *Server) handleAppBskyFeedGetAuthorFeed(ctx context.Context, author string, before string, limit int) (*appbskytypes.FeedGetAuthorFeed_Output, error) {
	panic("not yet implemented")
}

func (s *Server) handleAppBskyFeedGetPostThread(ctx context.Context, depth int, uri string) (*appbskytypes.FeedGetPostThread_Output, error) {
	panic("not yet implemented")
}

func (s *Server) handleAppBskyFeedGetRepostedBy(ctx context.Context, before string, cid string, limit int, uri string) (*appbskytypes.FeedGetRepostedBy_Output, error) {
	panic("not yet implemented")
}

func (s *Server) handleAppBskyFeedGetTimeline(ctx context.Context, algorithm string, before string, limit int) (*appbskytypes.FeedGetTimeline_Output, error) {
	panic("not yet implemented")
}

func (s *Server) handleAppBskyFeedGetVotes(ctx context.Context, before string, cid string, direction string, limit int, uri string) (*appbskytypes.FeedGetVotes_Output, error) {
	panic("not yet implemented")
}

func (s *Server) handleAppBskyFeedSetVote(ctx context.Context, input *appbskytypes.FeedSetVote_Input) (*appbskytypes.FeedSetVote_Output, error) {
	panic("not yet implemented")
}

func (s *Server) handleAppBskyGraphGetAssertions(ctx context.Context, assertion string, author string, before string) (*appbskytypes.GraphGetAssertions_Output, error) {
	panic("not yet implemented")
}

func (s *Server) handleAppBskyGraphGetFollowers(ctx context.Context, before string, limit int, user string) (*appbskytypes.GraphGetFollowers_Output, error) {
	panic("not yet implemented")
}

func (s *Server) handleAppBskyGraphGetFollows(ctx context.Context, before string, limit int, user string) (*appbskytypes.GraphGetFollows_Output, error) {
	panic("not yet implemented")
}

func (s *Server) handleAppBskyGraphGetMembers(ctx context.Context, actor string, before string, limit int) (*appbskytypes.GraphGetMembers_Output, error) {
	panic("not yet implemented")
}

func (s *Server) handleAppBskyGraphGetMemberships(ctx context.Context, actor string, before string, limit int) (*appbskytypes.GraphGetMemberships_Output, error) {
	panic("not yet implemented")
}

func (s *Server) handleAppBskyNotificationGetCount(ctx context.Context) (*appbskytypes.NotificationGetCount_Output, error) {
	panic("not yet implemented")
}

func (s *Server) handleAppBskyNotificationList(ctx context.Context, before string, limit int) (*appbskytypes.NotificationList_Output, error) {
	panic("not yet implemented")
}

func (s *Server) handleAppBskyNotificationUpdateSeen(ctx context.Context, input *appbskytypes.NotificationUpdateSeen_Input) error {
	panic("not yet implemented")
}

func (s *Server) handleComAtprotoAccountCreate(ctx context.Context, input *comatprototypes.AccountCreate_Input) (*comatprototypes.AccountCreate_Output, error) {
	if err := s.db.Create(&User{
		Handle:      input.Handle,
		Password:    input.Password,
		RecoveryKey: input.RecoveryKey,
	}).Error; err != nil {
		return nil, err
	}

	d, err := s.fakeDid.NewForHandle(input.Handle)
	if err != nil {
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
	panic("not yet implemented")
}

func (s *Server) handleComAtprotoAccountDelete(ctx context.Context) error {
	panic("not yet implemented")
}

func (s *Server) handleComAtprotoAccountGet(ctx context.Context) error {
	panic("not yet implemented")
}

func (s *Server) handleComAtprotoAccountRequestPasswordReset(ctx context.Context, input *comatprototypes.AccountRequestPasswordReset_Input) error {
	panic("not yet implemented")
}

func (s *Server) handleComAtprotoAccountResetPassword(ctx context.Context, input *comatprototypes.AccountResetPassword_Input) error {
	panic("not yet implemented")
}

func (s *Server) handleComAtprotoHandleResolve(ctx context.Context, handle string) (*comatprototypes.HandleResolve_Output, error) {
	panic("not yet implemented")
}

func (s *Server) handleComAtprotoRepoBatchWrite(ctx context.Context, input *comatprototypes.RepoBatchWrite_Input) error {
	panic("not yet implemented")
}

func (s *Server) handleComAtprotoRepoCreateRecord(ctx context.Context, input *comatprototypes.RepoCreateRecord_Input) (*comatprototypes.RepoCreateRecord_Output, error) {
	u, err := s.getUser(ctx)
	if err != nil {
		return nil, err
	}
}

func (s *Server) handleComAtprotoRepoDeleteRecord(ctx context.Context, input *comatprototypes.RepoDeleteRecord_Input) error {
	panic("not yet implemented")
}

func (s *Server) handleComAtprotoRepoDescribe(ctx context.Context, user string) (*comatprototypes.RepoDescribe_Output, error) {
	panic("not yet implemented")
}

func (s *Server) handleComAtprotoRepoGetRecord(ctx context.Context, cid string, collection string, rkey string, user string) (*comatprototypes.RepoGetRecord_Output, error) {
	panic("not yet implemented")
}

func (s *Server) handleComAtprotoRepoListRecords(ctx context.Context, after string, before string, collection string, limit int) (*comatprototypes.RepoListRecords_Output, error) {
	panic("not yet implemented")
}

func (s *Server) handleComAtprotoRepoPutRecord(ctx context.Context, input *comatprototypes.RepoPutRecord_Input) (*comatprototypes.RepoPutRecord_Output, error) {
	panic("not yet implemented")
}

func (s *Server) handleComAtprotoServerGetAccountsConfig(ctx context.Context) (*comatprototypes.ServerGetAccountsConfig_Output, error) {
	panic("not yet implemented")
}

func (s *Server) handleComAtprotoSessionCreate(ctx context.Context, input *comatprototypes.SessionCreate_Input) (*comatprototypes.SessionCreate_Output, error) {
	panic("not yet implemented")
}

func (s *Server) handleComAtprotoSessionDelete(ctx context.Context) error {
	panic("not yet implemented")
}

func (s *Server) handleComAtprotoSessionGet(ctx context.Context) (*comatprototypes.SessionGet_Output, error) {
	panic("not yet implemented")
}

func (s *Server) handleComAtprotoSessionRefresh(ctx context.Context) (*comatprototypes.SessionRefresh_Output, error) {
	panic("not yet implemented")
}

func (s *Server) handleComAtprotoSyncGetRepo(ctx context.Context, did string, from string) (io.Reader, error) {
	panic("not yet implemented")
}

func (s *Server) handleComAtprotoSyncGetRoot(ctx context.Context, did string) (*comatprototypes.SyncGetRoot_Output, error) {
	panic("not yet implemented")
}

func (s *Server) handleComAtprotoSyncUpdateRepo(ctx context.Context, r io.Reader) error {
	panic("not yet implemented")
}
