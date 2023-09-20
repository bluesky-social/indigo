package bgs

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"

	atproto "github.com/bluesky-social/indigo/api/atproto"
	comatprototypes "github.com/bluesky-social/indigo/api/atproto"
	"gorm.io/gorm"

	"github.com/bluesky-social/indigo/util"
	"github.com/bluesky-social/indigo/xrpc"
	"github.com/ipfs/go-cid"
	"github.com/labstack/echo/v4"
)

// Since the removal of repo history, this is the same as GetRepo but without the "since" bit
func (s *BGS) handleComAtprotoSyncGetCheckout(ctx context.Context, did string) (io.Reader, error) {
	u, err := s.Index.LookupUserByDid(ctx, did)
	if err != nil {
		return nil, err
	}

	// TODO: stream the response
	buf := new(bytes.Buffer)
	if err := s.repoman.ReadRepo(ctx, u.Uid, "", buf); err != nil {
		return nil, fmt.Errorf("failed to read repo: %w", err)
	}

	return buf, nil
}

func (s *BGS) handleComAtprotoSyncGetCommitPath(ctx context.Context, did string, earliest string, latest string) (*comatprototypes.SyncGetCommitPath_Output, error) {
	return nil, fmt.Errorf("nyi")
}

func (s *BGS) handleComAtprotoSyncGetHead(ctx context.Context, did string) (*comatprototypes.SyncGetHead_Output, error) {
	u, err := s.Index.LookupUserByDid(ctx, did)
	if err != nil {
		return nil, err
	}

	root, err := s.repoman.GetRepoRoot(ctx, u.Uid)
	if err != nil {
		return nil, err
	}

	return &comatprototypes.SyncGetHead_Output{
		Root: root.String(),
	}, nil
}

func (s *BGS) handleComAtprotoSyncGetRecord(ctx context.Context, collection string, commit string, did string, rkey string) (io.Reader, error) {
	u, err := s.Index.LookupUserByDid(ctx, did)
	if err != nil {
		return nil, fmt.Errorf("failed to find user: %w", err)
	}

	reqCid := cid.Undef
	if commit != "" {
		reqCid, err = cid.Decode(commit)
		if err != nil {
			return nil, fmt.Errorf("failed to decode commit cid: %w", err)
		}
	}

	_, record, err := s.repoman.GetRecord(ctx, u.Uid, collection, rkey, reqCid)
	if err != nil {
		return nil, fmt.Errorf("failed to get record: %w", err)
	}

	buf := new(bytes.Buffer)
	err = record.MarshalCBOR(buf)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal record: %w", err)
	}

	return buf, nil
}

func (s *BGS) handleComAtprotoSyncGetRepo(ctx context.Context, did string, since string) (io.Reader, error) {
	u, err := s.Index.LookupUserByDid(ctx, did)
	if err != nil {
		return nil, err
	}

	// TODO: stream the response
	buf := new(bytes.Buffer)
	if err := s.repoman.ReadRepo(ctx, u.Uid, since, buf); err != nil {
		return nil, fmt.Errorf("failed to read repo: %w", err)
	}

	return buf, nil
}

func (s *BGS) handleComAtprotoSyncGetBlocks(ctx context.Context, cids []string, did string) (io.Reader, error) {
	return nil, fmt.Errorf("NYI")
}

func (s *BGS) handleComAtprotoSyncRequestCrawl(ctx context.Context, body *comatprototypes.SyncRequestCrawl_Input) error {
	host := body.Hostname
	if host == "" {
		return fmt.Errorf("must pass valid hostname")
	}

	if strings.HasPrefix(host, "https://") || strings.HasPrefix(host, "http://") {
		return &echo.HTTPError{
			Code:    400,
			Message: "must pass domain without protocol scheme",
		}
	}

	norm, err := util.NormalizeHostname(host)
	if err != nil {
		return err
	}

	banned, err := s.domainIsBanned(ctx, host)
	if banned {
		return &echo.HTTPError{
			Code:    401,
			Message: "domain is banned",
		}
	}

	log.Warnf("TODO: better host validation for crawl requests")

	c := &xrpc.Client{
		Host:   "https://" + host,
		Client: http.DefaultClient, // not using the client that auto-retries
	}

	if !s.ssl {
		c.Host = "http://" + host
	}

	desc, err := atproto.ServerDescribeServer(ctx, c)
	if err != nil {
		return &echo.HTTPError{
			Code:    401,
			Message: fmt.Sprintf("given host failed to respond to ping: %s", err),
		}
	}

	// Maybe we could do something with this response later
	_ = desc

	return s.slurper.SubscribeToPds(ctx, norm, true)
}

func (s *BGS) handleComAtprotoSyncNotifyOfUpdate(ctx context.Context, body *comatprototypes.SyncNotifyOfUpdate_Input) error {
	// TODO:
	return nil
}

func (s *BGS) handleComAtprotoSyncGetBlob(ctx context.Context, cid string, did string) (io.Reader, error) {
	if s.blobs == nil {
		return nil, fmt.Errorf("blob store disabled")
	}

	b, err := s.blobs.GetBlob(ctx, cid, did)
	if err != nil {
		return nil, err
	}

	return bytes.NewReader(b), nil
}

func (s *BGS) handleComAtprotoSyncListBlobs(ctx context.Context, cursor string, did string, limit int, since string) (*comatprototypes.SyncListBlobs_Output, error) {
	return nil, fmt.Errorf("NYI")
}

func (s *BGS) handleComAtprotoSyncListRepos(ctx context.Context, cursor string, limit int) (*comatprototypes.SyncListRepos_Output, error) {
	if limit > 1000 {
		limit = 1000
	}

	// Use UIDs for the cursor
	var err error
	c := int64(0)
	if cursor != "" {
		c, err = strconv.ParseInt(cursor, 10, 64)
		if err != nil {
			return nil, fmt.Errorf("invalid cursor: %w", err)
		}
	}

	users := []User{}
	if err := s.db.Model(&User{}).Where("uid > ?", c).Limit(limit).Find(&users).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			return &comatprototypes.SyncListRepos_Output{}, nil
		}
		return nil, fmt.Errorf("failed to get users: %w", err)
	}

	if len(users) == 0 {
		return &comatprototypes.SyncListRepos_Output{}, nil
	}

	resp := &comatprototypes.SyncListRepos_Output{
		Repos: []*comatprototypes.SyncListRepos_Repo{},
	}

	for i := range users {
		user := users[i]
		root, err := s.repoman.GetRepoRoot(ctx, user.ID)
		if err != nil {
			return nil, fmt.Errorf("failed to get repo root for (%s): %w", user.Did, err)
		}

		resp.Repos = append(resp.Repos, &comatprototypes.SyncListRepos_Repo{
			Did:  user.Did,
			Head: root.String(),
		})
	}

	c += int64(len(users))
	cursor = strconv.FormatInt(c, 10)
	resp.Cursor = &cursor

	return resp, nil
}

func (s *BGS) handleComAtprotoSyncGetLatestCommit(ctx context.Context, did string) (*comatprototypes.SyncGetLatestCommit_Output, error) {
	u, err := s.Index.LookupUserByDid(ctx, did)
	if err != nil {
		return nil, fmt.Errorf("failed to find user: %w", err)
	}

	root, err := s.repoman.GetRepoRoot(ctx, u.Uid)
	if err != nil {
		return nil, fmt.Errorf("failed to get repo root: %w", err)
	}

	rev, err := s.repoman.GetRepoRev(ctx, u.Uid)
	if err != nil {
		return nil, fmt.Errorf("failed to get repo rev: %w", err)
	}

	return &comatprototypes.SyncGetLatestCommit_Output{
		Cid: root.String(),
		Rev: rev,
	}, nil
}
