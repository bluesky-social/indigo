package pds

import (
	"bytes"
	"context"
	"database/sql"
	"fmt"
	"io"
	"maps"
	"slices"
	"strings"
	"time"

	comatprototypes "github.com/bluesky-social/indigo/api/atproto"
	"github.com/bluesky-social/indigo/carstore"
	"github.com/bluesky-social/indigo/events"
	lexutil "github.com/bluesky-social/indigo/lex/util"
	"github.com/bluesky-social/indigo/models"
	"github.com/bluesky-social/indigo/repo"
	"github.com/bluesky-social/indigo/util"
	"github.com/ipfs/go-cid"
	"github.com/labstack/echo/v4"
	"github.com/lestrrat-go/jwx/v2/jwt"
)

func (s *Server) handleComAtprotoServerCreateAccount(ctx context.Context, body *comatprototypes.ServerCreateAccount_Input) (*comatprototypes.ServerCreateAccount_Output, error) {
	if body.Email == nil {
		return nil, fmt.Errorf("email is required")
	}

	if body.Password == nil {
		return nil, fmt.Errorf("password is required")
	}

	if err := validateEmail(*body.Email); err != nil {
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

	// if recoveryKey == "" {
	// 	recoveryKey = s.signingKey.Public().DID()
	// }

	d, err := s.plc.CreateDID(ctx, s.signingKey, recoveryKey, body.Handle, s.serviceUrl)
	if err != nil {
		return nil, fmt.Errorf("create did: %w", err)
	}

	u := User{
		Handle:      body.Handle,
		Password:    *body.Password,
		RecoveryKey: recoveryKey,
		Email:       *body.Email,
		Did:         d,
	}
	if err := s.db.Model(&User{}).Create(&u).Error; err != nil {
		return nil, err
	}

	ai := &models.ActorInfo{
		Uid:    u.ID,
		Did:    d,
		Handle: sql.NullString{String: body.Handle, Valid: true},
	}
	if err := s.db.Create(ai).Error; err != nil {
		return nil, err
	}

	if err := s.repoman.InitNewActor(ctx, u.ID, u.Handle, u.Did, "", "", ""); err != nil {
		return nil, err
	}

	if err := s.events.AddEvent(ctx, &events.XRPCStreamEvent{
		RepoAccount: &comatprototypes.SyncSubscribeRepos_Account{
			Active: true,
			Did:    d,
			Time:   time.Now().Format(util.ISO8601),
		},
	}); err != nil {
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

func (s *Server) handleComAtprotoRepoGetRecord(ec echo.Context, ctx context.Context, c string, collection string, repo string, rkey string) (*comatprototypes.RepoGetRecord_Output, error) {
	targetUser, err := s.lookupUser(ctx, repo)
	if err != nil {
		return nil, s.HandleAppViewProxy(ec)
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
		return nil, err
	}

	ccstr := reccid.String()
	return &comatprototypes.RepoGetRecord_Output{
		Cid:   &ccstr,
		Uri:   "at://" + targetUser.Did + "/" + collection + "/" + rkey,
		Value: &lexutil.LexiconTypeDecoder{Val: rec},
	}, nil
}

func (s *Server) handleComAtprotoRepoListRecords(ctx context.Context, collection string, cursor string, limit int, repoId string, reverse *bool, rkeyEnd string, rkeyStart string) (*comatprototypes.RepoListRecords_Output, error) {
	var user User
	if err := s.db.Model(&User{}).
		Where("did = ? OR handle = ?", repoId, repoId).
		Find(&user).Error; err != nil {
		return nil, err
	}

	head, err := s.repoman.GetRepoRoot(ctx, user.ID)
	if err != nil {
		return nil, err
	}

	ds, err := s.repoman.CarStore().ReadOnlySession(user.ID)
	if err != nil {
		return nil, err
	}

	repo, err := repo.OpenRepo(ctx, ds, head)
	if err != nil {
		return nil, err
	}

	var records []*comatprototypes.RepoListRecords_Record

	if err := repo.ForEach(ctx, collection, func(k string, v cid.Cid) error {
		_, val, err := repo.GetRecord(ctx, k)
		if err != nil {
			return err
		}

		if !strings.HasPrefix(k, collection) {
			return nil
		}

		record := &comatprototypes.RepoListRecords_Record{
			Cid: v.String(),
			Uri: "at://" + repoId + "/" + k,
			Value: &lexutil.LexiconTypeDecoder{
				Val: val,
			},
		}

		records = append(records, record)
		return nil
	}); err != nil {
		return nil, err
	}

	return &comatprototypes.RepoListRecords_Output{
		Records: records,
	}, nil
}

func (s *Server) handleComAtprotoRepoPutRecord(ctx context.Context, input *comatprototypes.RepoPutRecord_Input) (*comatprototypes.RepoPutRecord_Output, error) {
	panic("not yet implemented")
}

func (s *Server) handleComAtprotoServerDescribeServer(ctx context.Context) (*comatprototypes.ServerDescribeServer_Output, error) {
	invcode := false
	return &comatprototypes.ServerDescribeServer_Output{
		InviteCodeRequired:   &invcode,
		AvailableUserDomains: []string{"dev.ellie.fm"},
		Did:                  "did:web:" + s.serviceUrl,
		Links:                &comatprototypes.ServerDescribeServer_Links{},
	}, nil
}

var ErrInvalidUsernameOrPassword = fmt.Errorf("invalid username or password")

func (s *Server) handleComAtprotoServerCreateSession(ctx context.Context, body *comatprototypes.ServerCreateSession_Input) (*comatprototypes.ServerCreateSession_Output, error) {
	u, err := s.lookupUserByHandle(ctx, body.Identifier)
	if err != nil {
		return nil, err
	}

	if body.Password != u.Password {
		return nil, ErrInvalidUsernameOrPassword
	}

	tok, err := s.createAuthTokenForUser(ctx, body.Identifier, u.Did)
	if err != nil {
		return nil, err
	}

	status := "true"
	didDoc, err := s.plc.GetDocument(ctx, u.Did)
	if err != nil {
		return nil, err
	}
	genericDoc := interface{}(didDoc)

	return &comatprototypes.ServerCreateSession_Output{
		Handle:     body.Identifier,
		Did:        u.Did,
		AccessJwt:  tok.AccessJwt,
		RefreshJwt: tok.RefreshJwt,
		Status:     &status,
		DidDoc:     &genericDoc,
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
	didDoc, err := s.plc.GetDocument(ctx, u.Did)
	if err != nil {
		return nil, err
	}

	email := "foo@bar.com"
	genericDoc := interface{}(didDoc)
	active := true
	return &comatprototypes.ServerGetSession_Output{
		Handle: u.Handle,
		Did:    u.Did,
		Email:  &email,
		DidDoc: &genericDoc,
		Active: &active,
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

func (s *Server) handleComAtprotoSyncGetCheckout(ctx context.Context, did string) (io.Reader, error) {
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

func (s *Server) handleComAtprotoSyncGetRecord(ctx context.Context, repoId string, collection string, commit string, did string, rkey string) (io.Reader, error) {
	var user User
	if err := s.db.Model(&User{}).
		Where("did = ?", did).
		Find(&user).Error; err != nil {
		return nil, err
	}

	ds, err := s.repoman.CarStore().ReadOnlySession(user.ID)
	if err != nil {
		return nil, err
	}

	head, err := s.repoman.CarStore().GetUserRepoHead(ctx, user.ID)
	if err != nil {
		return nil, err
	}

	r, err := repo.OpenRepo(ctx, ds, head)
	if err != nil {
		return nil, err
	}

	buf := bytes.NewBuffer(nil)

	if _, err := carstore.WriteCarHeader(buf, head); err != nil {
		return nil, err
	}

	block, err := r.Blockstore().Get(ctx, head)
	if err != nil {
		return nil, err
	}

	carstore.LdWrite(buf, head.Bytes(), block.RawData())

	mst, err := r.GetMst(ctx)
	if err != nil {
		return nil, err
	}

	cids, err := mst.CidsForPath(ctx, collection+"/"+rkey)
	if err != nil {
		return nil, err
	}

	uniqCids := make(map[cid.Cid]bool)
	for _, cid := range cids {
		uniqCids[cid] = true
	}

	for cid := range maps.Keys(uniqCids) {
		block, err := r.Blockstore().Get(ctx, cid)
		if err != nil {
			return buf, err
		}

		if _, err := carstore.LdWrite(buf, cid.Bytes(), block.RawData()); err != nil {
			return buf, err
		}
	}

	c, _ := mst.Get(ctx, collection+"/"+rkey)

	block, err = r.Blockstore().Get(ctx, c)
	if err != nil {
		return buf, err
	}

	if _, err := carstore.LdWrite(buf, c.Bytes(), block.RawData()); err != nil {
		return buf, err
	}

	return buf, nil
}

func (s *Server) handleComAtprotoSyncGetRepo(ctx context.Context, did string, since string) (io.Reader, error) {
	targetUser, err := s.lookupUser(ctx, did)
	if err != nil {
		return nil, err
	}

	buf := new(bytes.Buffer)
	if err := s.repoman.ReadRepo(ctx, targetUser.ID, since, buf); err != nil {
		return nil, err
	}

	return buf, nil
}

func (s *Server) handleComAtprotoSyncGetBlocks(ctx context.Context, cids []string, did string) (io.Reader, error) {
	panic("nyi")
}

func (s *Server) handleComAtprotoSyncNotifyOfUpdate(ctx context.Context, body *comatprototypes.SyncNotifyOfUpdate_Input) error {
	panic("nyi")
}

func (s *Server) handleComAtprotoSyncRequestCrawl(ctx context.Context, body *comatprototypes.SyncRequestCrawl_Input) error {
	panic("nyi")
}

func (s *Server) handleComAtprotoSyncGetBlob(ctx context.Context, cid string, did string) (io.Reader, error) {
	panic("nyi")
}

func (s *Server) handleComAtprotoSyncListBlobs(ctx context.Context, cursor string, did string, limit int, since string) (*comatprototypes.SyncListBlobs_Output, error) {
	panic("nyi")
}

func (s *Server) handleComAtprotoIdentityUpdateHandle(ctx context.Context, body *comatprototypes.IdentityUpdateHandle_Input) error {
	// if err := s.validateHandle(body.Handle); err != nil {
	// 	return err
	// }

	u, err := s.getUser(ctx)
	if err != nil {
		return err
	}

	return s.UpdateUserHandle(ctx, u, body.Handle)
}

func (s *Server) handleComAtprotoModerationCreateReport(ctx context.Context, body *comatprototypes.ModerationCreateReport_Input) (*comatprototypes.ModerationCreateReport_Output, error) {
	panic("nyi")
}

func (s *Server) handleComAtprotoRepoDescribeRepo(ctx context.Context, repoId string) (*comatprototypes.RepoDescribeRepo_Output, error) {
	var user User
	if err := s.db.Model(&User{}).
		Where("did = ? OR handle = ?", repoId, repoId).
		Find(&user).Error; err != nil {
		return nil, err
	}

	didDoc, err := s.plc.GetDocument(ctx, user.Did)
	if err != nil {
		return nil, err
	}

	head, err := s.repoman.GetRepoRoot(ctx, user.ID)
	if err != nil {
		return nil, err
	}

	ds, err := s.repoman.CarStore().ReadOnlySession(user.ID)
	if err != nil {
		return nil, err
	}

	repo, err := repo.OpenRepo(ctx, ds, head)
	if err != nil {
		return nil, err
	}

	collectionTypes := make(map[string]bool)

	if err := repo.ForEach(ctx, "", func(k string, v cid.Cid) error {
		parts := strings.Split(k, "/")
		if len(parts) < 1 {
			return fmt.Errorf("malformed key type")
		}

		collectionTypes[parts[0]] = true

		return nil
	}); err != nil {
		return nil, err
	}

	resp := &comatprototypes.RepoDescribeRepo_Output{
		Collections:     slices.Sorted(maps.Keys(collectionTypes)),
		Handle:          user.Handle,
		Did:             user.Did,
		DidDoc:          didDoc,
		HandleIsCorrect: true,
	}
	return resp, nil
}

func (s *Server) handleComAtprotoAdminDisableInviteCodes(ctx context.Context, body *comatprototypes.AdminDisableInviteCodes_Input) error {
	panic("nyi")
}

func (s *Server) handleComAtprotoAdminGetInviteCodes(ctx context.Context, cursor string, limit int, sort string) (*comatprototypes.AdminGetInviteCodes_Output, error) {
	panic("nyi")
}

func (s *Server) handleComAtprotoLabelQueryLabels(ctx context.Context, cursor string, limit int, sources []string, uriPatterns []string) (*comatprototypes.LabelQueryLabels_Output, error) {
	panic("nyi")
}

func (s *Server) handleComAtprotoServerCreateInviteCodes(ctx context.Context, body *comatprototypes.ServerCreateInviteCodes_Input) (*comatprototypes.ServerCreateInviteCodes_Output, error) {
	panic("nyi")
}

func (s *Server) handleComAtprotoServerGetAccountInviteCodes(ctx context.Context, createAvailable bool, includeUsed bool) (*comatprototypes.ServerGetAccountInviteCodes_Output, error) {
	panic("nyi")
}

func (s *Server) handleComAtprotoSyncListRepos(ctx context.Context, cursor string, limit int) (*comatprototypes.SyncListRepos_Output, error) {
	repos := []*comatprototypes.SyncListRepos_Repo{}

	var users []User

	if err := s.db.Model(&User{}).Find(&users).Error; err != nil {
		return nil, err
	}

	for _, user := range users {
		active := true
		rev, err := s.repoman.GetRepoRev(ctx, user.ID)
		if err != nil {
			return nil, err
		}
		head, err := s.repoman.GetRepoRoot(ctx, user.ID)
		if err != nil {
			return nil, err
		}

		repo := &comatprototypes.SyncListRepos_Repo{
			Active: &active,
			Did:    user.Did,
			Head:   head.String(),
			Rev:    rev,
		}

		repos = append(repos, repo)
	}

	resp := &comatprototypes.SyncListRepos_Output{
		Repos: repos,
	}

	return resp, nil
}

func (s *Server) handleComAtprotoAdminUpdateAccountEmail(ctx context.Context, body *comatprototypes.AdminUpdateAccountEmail_Input) error {
	panic("nyi")
}

func (s *Server) handleComAtprotoAdminUpdateAccountHandle(ctx context.Context, body *comatprototypes.AdminUpdateAccountHandle_Input) error {
	panic("nyi")
}

func (s *Server) handleComAtprotoServerCreateAppPassword(ctx context.Context, body *comatprototypes.ServerCreateAppPassword_Input) (*comatprototypes.ServerCreateAppPassword_AppPassword, error) {
	panic("nyi")
}

func (s *Server) handleComAtprotoServerListAppPasswords(ctx context.Context) (*comatprototypes.ServerListAppPasswords_Output, error) {
	panic("nyi")
}

func (s *Server) handleComAtprotoServerRevokeAppPassword(ctx context.Context, body *comatprototypes.ServerRevokeAppPassword_Input) error {
	panic("nyi")
}

func (s *Server) handleComAtprotoAdminDisableAccountInvites(ctx context.Context, body *comatprototypes.AdminDisableAccountInvites_Input) error {
	panic("nyi")
}

func (s *Server) handleComAtprotoAdminEnableAccountInvites(ctx context.Context, body *comatprototypes.AdminEnableAccountInvites_Input) error {
	panic("nyi")
}

func (s *Server) handleComAtprotoAdminSendEmail(ctx context.Context, body *comatprototypes.AdminSendEmail_Input) (*comatprototypes.AdminSendEmail_Output, error) {
	panic("nyi")
}

func (s *Server) handleComAtprotoSyncGetLatestCommit(ctx context.Context, did string) (*comatprototypes.SyncGetLatestCommit_Output, error) {
	var user User
	if err := s.db.Model(&User{}).
		Where("did = ?", did).
		Find(&user).Error; err != nil {
		return nil, err
	}

	rev, err := s.repoman.GetRepoRev(ctx, user.ID)
	if err != nil {
		return nil, err
	}
	head, err := s.repoman.GetRepoRoot(ctx, user.ID)
	if err != nil {
		return nil, err
	}

	return &comatprototypes.SyncGetLatestCommit_Output{
		Cid: head.String(),
		Rev: rev,
	}, nil
}

func (s *Server) handleComAtprotoAdminGetAccountInfo(ctx context.Context, did string) (*comatprototypes.AdminDefs_AccountView, error) {
	panic("nyi")
}

func (s *Server) handleComAtprotoAdminGetSubjectStatus(ctx context.Context, blob string, did string, uri string) (*comatprototypes.AdminGetSubjectStatus_Output, error) {
	panic("nyi")
}

func (s *Server) handleComAtprotoAdminUpdateSubjectStatus(ctx context.Context, body *comatprototypes.AdminUpdateSubjectStatus_Input) (*comatprototypes.AdminUpdateSubjectStatus_Output, error) {
	panic("nyi")
}

func (s *Server) handleComAtprotoServerConfirmEmail(ctx context.Context, body *comatprototypes.ServerConfirmEmail_Input) error {
	panic("nyi")
}

func (s *Server) handleComAtprotoServerRequestEmailConfirmation(ctx context.Context) error {
	panic("nyi")
}

func (s *Server) handleComAtprotoServerRequestEmailUpdate(ctx context.Context) (*comatprototypes.ServerRequestEmailUpdate_Output, error) {
	panic("nyi")
}

func (s *Server) handleComAtprotoServerReserveSigningKey(ctx context.Context, body *comatprototypes.ServerReserveSigningKey_Input) (*comatprototypes.ServerReserveSigningKey_Output, error) {
	panic("nyi")
}

func (s *Server) handleComAtprotoServerUpdateEmail(ctx context.Context, body *comatprototypes.ServerUpdateEmail_Input) error {
	panic("nyi")
}

func (s *Server) handleComAtprotoTempFetchLabels(ctx context.Context, limit int, since *int) (*comatprototypes.TempFetchLabels_Output, error) {
	panic("nyi")
}
