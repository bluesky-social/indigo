package bgs

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"

	atproto "github.com/bluesky-social/indigo/api/atproto"
	comatprototypes "github.com/bluesky-social/indigo/api/atproto"
	"github.com/bluesky-social/indigo/blobs"
	"github.com/bluesky-social/indigo/mst"
	"gorm.io/gorm"

	"github.com/bluesky-social/indigo/util"
	"github.com/bluesky-social/indigo/xrpc"
	"github.com/ipfs/go-cid"
	"github.com/labstack/echo/v4"
)

func (s *BGS) handleComAtprotoSyncGetRecord(ctx context.Context, collection string, commit string, did string, rkey string) (io.Reader, error) {
	u, err := s.Index.LookupUserByDid(ctx, did)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, echo.NewHTTPError(http.StatusNotFound, "user not found")
		}
		return nil, echo.NewHTTPError(http.StatusInternalServerError, "failed to lookup user")
	}

	reqCid := cid.Undef
	if commit != "" {
		reqCid, err = cid.Decode(commit)
		if err != nil {
			return nil, echo.NewHTTPError(http.StatusBadRequest, "failed to decode commit cid")
		}
	}

	_, record, err := s.repoman.GetRecord(ctx, u.Uid, collection, rkey, reqCid)
	if err != nil {
		if errors.Is(err, mst.ErrNotFound) {
			return nil, echo.NewHTTPError(http.StatusNotFound, "record not found in repo")
		}
		return nil, echo.NewHTTPError(http.StatusInternalServerError, "failed to get record from repo")
	}

	buf := new(bytes.Buffer)
	err = record.MarshalCBOR(buf)
	if err != nil {
		return nil, echo.NewHTTPError(http.StatusInternalServerError, "failed to marshal record to CBOR")
	}

	return buf, nil
}

func (s *BGS) handleComAtprotoSyncGetRepo(ctx context.Context, did string, since string) (io.Reader, error) {
	u, err := s.Index.LookupUserByDid(ctx, did)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, echo.NewHTTPError(http.StatusNotFound, "user not found")
		}
		return nil, echo.NewHTTPError(http.StatusInternalServerError, "failed to lookup user")
	}

	// TODO: stream the response
	buf := new(bytes.Buffer)
	if err := s.repoman.ReadRepo(ctx, u.Uid, since, buf); err != nil {
		return nil, echo.NewHTTPError(http.StatusInternalServerError, "failed to read repo into buffer")
	}

	return buf, nil
}

func (s *BGS) handleComAtprotoSyncGetBlocks(ctx context.Context, cids []string, did string) (io.Reader, error) {
	return nil, fmt.Errorf("NYI")
}

func (s *BGS) handleComAtprotoSyncRequestCrawl(ctx context.Context, body *comatprototypes.SyncRequestCrawl_Input) error {
	host := body.Hostname
	if host == "" {
		return echo.NewHTTPError(http.StatusBadRequest, "must pass hostname")
	}

	if strings.HasPrefix(host, "https://") || strings.HasPrefix(host, "http://") {
		return echo.NewHTTPError(http.StatusBadRequest, "must pass domain without protocol scheme")
	}

	norm, err := util.NormalizeHostname(host)
	if err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, "failed to normalize hostname")
	}

	banned, err := s.domainIsBanned(ctx, host)
	if banned {
		return echo.NewHTTPError(http.StatusForbidden, "domain is banned")
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
		return echo.NewHTTPError(http.StatusInternalServerError, "requested host failed to respond to describe request")
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
		return nil, echo.NewHTTPError(http.StatusNotFound, "blobs not enabled on this server")
	}

	b, err := s.blobs.GetBlob(ctx, cid, did)
	if err != nil {
		if errors.Is(err, blobs.NotFoundErr) {
			return nil, echo.NewHTTPError(http.StatusNotFound, "blob not found")
		}
		return nil, echo.NewHTTPError(http.StatusInternalServerError, "failed to get blob")
	}

	return bytes.NewReader(b), nil
}

func (s *BGS) handleComAtprotoSyncListBlobs(ctx context.Context, cursor string, did string, limit int, since string) (*comatprototypes.SyncListBlobs_Output, error) {
	return nil, fmt.Errorf("NYI")
}

func (s *BGS) handleComAtprotoSyncListRepos(ctx context.Context, cursor string, limit int) (*comatprototypes.SyncListRepos_Output, error) {
	// Use UIDs for the cursor
	var err error
	c := int64(0)
	if cursor != "" {
		c, err = strconv.ParseInt(cursor, 10, 64)
		if err != nil {
			return nil, echo.NewHTTPError(http.StatusBadRequest, "couldn't parse your cursor as an integer")
		}
	}

	users := []User{}
	if err := s.db.Model(&User{}).Where("id > ?", c).Order("id").Limit(limit).Find(&users).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			return &comatprototypes.SyncListRepos_Output{}, nil
		}
		return nil, echo.NewHTTPError(http.StatusInternalServerError, "failed to query users")
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
			log.Errorw("failed to get repo root", "err", err, "did", user.Did)
			return nil, echo.NewHTTPError(http.StatusInternalServerError, fmt.Sprintf("failed to get repo root for (%s): %v", user.Did, err.Error()))
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
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, echo.NewHTTPError(http.StatusNotFound, "user not found")
		}
		return nil, echo.NewHTTPError(http.StatusInternalServerError, "failed to lookup user")
	}

	root, err := s.repoman.GetRepoRoot(ctx, u.Uid)
	if err != nil {
		log.Errorw("failed to get repo root", "err", err, "did", u.Did)
		return nil, echo.NewHTTPError(http.StatusInternalServerError, "failed to get repo root")
	}

	rev, err := s.repoman.GetRepoRev(ctx, u.Uid)
	if err != nil {
		log.Errorw("failed to get repo rev", "err", err, "did", u.Did)
		return nil, echo.NewHTTPError(http.StatusInternalServerError, "failed to get repo rev")
	}

	return &comatprototypes.SyncGetLatestCommit_Output{
		Cid: root.String(),
		Rev: rev,
	}, nil
}
