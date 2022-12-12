package schemagen

import (
	"bytes"
	"context"
	"fmt"
	"io"

	"github.com/ipfs/go-cid"
	cbg "github.com/whyrusleeping/cbor-gen"
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
	_, err := s.getUser(ctx)
	if err != nil {
		return nil, err
	}

	target, err := s.lookupUser(ctx, author)
	//target, err := s.lookupUserByHandle(ctx, author)
	if err != nil {
		return nil, err
	}

	feed, err := s.feedgen.GetAuthorFeed(ctx, target.ID, before, limit)
	if err != nil {
		return nil, err
	}

	var out appbskytypes.FeedGetAuthorFeed_Output
	for _, fi := range feed {
		out.Feed = append(out.Feed, &appbskytypes.FeedGetAuthorFeed_FeedItem{
			Uri:           fi.Uri,
			RepostedBy:    fi.RepostedBy,
			Record:        fi.Record,
			ReplyCount:    fi.ReplyCount,
			RepostCount:   fi.RepostCount,
			UpvoteCount:   fi.UpvoteCount,
			DownvoteCount: 0,
			MyState:       nil, // TODO:
			Cid:           fi.Cid,
			Author:        fi.Author,
			TrendedBy:     fi.TrendedBy,
			Embed:         nil,
			IndexedAt:     fi.IndexedAt,
		})
	}

	return &out, nil
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

	if err := validateEmail(input.Email); err != nil {
		return nil, err
	}

	if err := s.validateHandle(input.Handle); err != nil {
		return nil, err

	}

	u := User{
		Handle:      input.Handle,
		Password:    input.Password,
		RecoveryKey: input.RecoveryKey,
		Email:       input.Email,
	}
	if err := s.db.Create(&u).Error; err != nil {
		return nil, err
	}

	d, err := s.fakeDid.NewForHandle(input.Handle)
	if err != nil {
		return nil, err
	}

	if err := s.repoman.InitNewActor(ctx, u.ID, u.Handle, u.DID, "", UserActorDeclCid, UserActorDeclType); err != nil {
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

	var rec cbg.CBORMarshaler
	switch input.Collection {
	case "app.bsky.feed.post":
		rec = new(appbskytypes.FeedPost)
	default:
		return nil, fmt.Errorf("unsupported collection: %q", input.Collection)
	}

	// TODO: if we had a 'record' type receiver declaration in lexicon i could
	// codegen in a special handler for things that are supposed to be records
	// like this
	if err := convertRecordTo(input.Record, rec); err != nil {
		return nil, err
	}

	rkey, recid, err := s.repoman.CreateRecord(ctx, u.ID, input.Collection, rec)
	if err != nil {
		return nil, err
	}

	return &comatprototypes.RepoCreateRecord_Output{
		Uri: "at://" + u.DID + "/" + rkey,
		Cid: recid.String(),
	}, nil
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
	u, err := s.lookupUserByHandle(ctx, input.Handle)
	if err != nil {
		return nil, err
	}

	if input.Password != u.Password {
		return nil, fmt.Errorf("invalid username or password")
	}

	tok, err := s.createAuthTokenForUser(ctx, input.Handle, u.DID)
	if err != nil {
		return nil, err
	}

	return &comatprototypes.SessionCreate_Output{
		Handle:     input.Handle,
		Did:        u.DID,
		AccessJwt:  tok.AccessJwt,
		RefreshJwt: tok.RefreshJwt,
	}, nil
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
	panic("not yet implemented")
}

func (s *Server) handleComAtprotoSyncUpdateRepo(ctx context.Context, r io.Reader) error {
	panic("not yet implemented")
}
