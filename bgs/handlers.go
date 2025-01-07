package bgs

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"

	atproto "github.com/bluesky-social/indigo/api/atproto"
	comatprototypes "github.com/bluesky-social/indigo/api/atproto"
	"github.com/bluesky-social/indigo/carstore"
	"github.com/bluesky-social/indigo/events"
	"github.com/bluesky-social/indigo/mst"
	"gorm.io/gorm"

	"github.com/bluesky-social/indigo/xrpc"
	"github.com/ipfs/go-cid"
	cbor "github.com/ipfs/go-ipld-cbor"
	"github.com/ipld/go-car"
	"github.com/labstack/echo/v4"
)

func (s *BGS) handleComAtprotoSyncGetRecord(ctx context.Context, collection string, did string, rkey string) (io.Reader, error) {
	u, err := s.lookupUserByDid(ctx, did)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, echo.NewHTTPError(http.StatusNotFound, "user not found")
		}
		log.Error("failed to lookup user", "err", err, "did", did)
		return nil, echo.NewHTTPError(http.StatusInternalServerError, "failed to lookup user")
	}

	if u.GetTombstoned() {
		return nil, fmt.Errorf("account was deleted")
	}

	if u.GetTakenDown() {
		return nil, fmt.Errorf("account was taken down by the Relay")
	}

	ustatus := u.GetUpstreamStatus()
	if ustatus == events.AccountStatusTakendown {
		return nil, fmt.Errorf("account was taken down by its PDS")
	}

	if ustatus == events.AccountStatusDeactivated {
		return nil, fmt.Errorf("account is temporarily deactivated")
	}

	if ustatus == events.AccountStatusSuspended {
		return nil, fmt.Errorf("account is suspended by its PDS")
	}

	root, blocks, err := s.repoman.GetRecordProof(ctx, u.ID, collection, rkey)
	if err != nil {
		if errors.Is(err, mst.ErrNotFound) {
			return nil, echo.NewHTTPError(http.StatusNotFound, "record not found in repo")
		}
		log.Error("failed to get record from repo", "err", err, "did", did, "collection", collection, "rkey", rkey)
		return nil, echo.NewHTTPError(http.StatusInternalServerError, "failed to get record from repo")
	}

	buf := new(bytes.Buffer)
	hb, err := cbor.DumpObject(&car.CarHeader{
		Roots:   []cid.Cid{root},
		Version: 1,
	})
	if _, err := carstore.LdWrite(buf, hb); err != nil {
		return nil, err
	}

	for _, blk := range blocks {
		if _, err := carstore.LdWrite(buf, blk.Cid().Bytes(), blk.RawData()); err != nil {
			return nil, err
		}
	}

	return buf, nil
}

func (s *BGS) handleComAtprotoSyncGetRepo(ctx context.Context, did string, since string) (io.Reader, error) {
	u, err := s.lookupUserByDid(ctx, did)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, echo.NewHTTPError(http.StatusNotFound, "user not found")
		}
		log.Error("failed to lookup user", "err", err, "did", did)
		return nil, echo.NewHTTPError(http.StatusInternalServerError, "failed to lookup user")
	}

	if u.GetTombstoned() {
		return nil, fmt.Errorf("account was deleted")
	}

	if u.GetTakenDown() {
		return nil, fmt.Errorf("account was taken down by the Relay")
	}

	ustatus := u.GetUpstreamStatus()
	if ustatus == events.AccountStatusTakendown {
		return nil, fmt.Errorf("account was taken down by its PDS")
	}

	if ustatus == events.AccountStatusDeactivated {
		return nil, fmt.Errorf("account is temporarily deactivated")
	}

	if ustatus == events.AccountStatusSuspended {
		return nil, fmt.Errorf("account is suspended by its PDS")
	}

	// TODO: stream the response
	buf := new(bytes.Buffer)
	if err := s.repoman.ReadRepo(ctx, u.ID, since, buf); err != nil {
		log.Error("failed to read repo into buffer", "err", err, "did", did)
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

	if !strings.HasPrefix(host, "http://") && !strings.HasPrefix(host, "https://") {
		if s.ssl {
			host = "https://" + host
		} else {
			host = "http://" + host
		}
	}

	u, err := url.Parse(host)
	if err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, "failed to parse hostname")
	}

	if u.Scheme == "http" && s.ssl {
		return echo.NewHTTPError(http.StatusBadRequest, "this server requires https")
	}

	if u.Scheme == "https" && !s.ssl {
		return echo.NewHTTPError(http.StatusBadRequest, "this server does not support https")
	}

	if u.Path != "" {
		return echo.NewHTTPError(http.StatusBadRequest, "must pass hostname without path")
	}

	if u.Query().Encode() != "" {
		return echo.NewHTTPError(http.StatusBadRequest, "must pass hostname without query")
	}

	host = u.Host // potentially hostname:port

	banned, err := s.domainIsBanned(ctx, host)
	if banned {
		return echo.NewHTTPError(http.StatusUnauthorized, "domain is banned")
	}

	log.Warn("TODO: better host validation for crawl requests")

	clientHost := fmt.Sprintf("%s://%s", u.Scheme, host)

	c := &xrpc.Client{
		Host:   clientHost,
		Client: http.DefaultClient, // not using the client that auto-retries
	}

	desc, err := atproto.ServerDescribeServer(ctx, c)
	if err != nil {
		errMsg := fmt.Sprintf("requested host (%s) failed to respond to describe request", clientHost)
		return echo.NewHTTPError(http.StatusBadRequest, errMsg)
	}

	// Maybe we could do something with this response later
	_ = desc

	if len(s.nextCrawlers) != 0 {
		blob, err := json.Marshal(body)
		if err != nil {
			log.Warn("could not forward requestCrawl, json err", "err", err)
		} else {
			go func(bodyBlob []byte) {
				for _, rpu := range s.nextCrawlers {
					pu := rpu.JoinPath("/xrpc/com.atproto.sync.requestCrawl")
					response, err := s.httpClient.Post(pu.String(), "application/json", bytes.NewReader(bodyBlob))
					if response != nil && response.Body != nil {
						response.Body.Close()
					}
					if err != nil || response == nil {
						log.Warn("requestCrawl forward failed", "host", rpu, "err", err)
					} else if response.StatusCode != http.StatusOK {
						log.Warn("requestCrawl forward failed", "host", rpu, "status", response.Status)
					} else {
						log.Info("requestCrawl forward successful", "host", rpu)
					}
				}
			}(blob)
		}
	}

	return s.slurper.SubscribeToPds(ctx, host, true, false, nil)
}

func (s *BGS) handleComAtprotoSyncNotifyOfUpdate(ctx context.Context, body *comatprototypes.SyncNotifyOfUpdate_Input) error {
	// TODO:
	return nil
}

func (s *BGS) handleComAtprotoSyncListRepos(ctx context.Context, cursor int64, limit int) (*comatprototypes.SyncListRepos_Output, error) {
	// Filter out tombstoned, taken down, and deactivated accounts
	q := fmt.Sprintf("id > ? AND NOT tombstoned AND NOT taken_down AND (upstream_status is NULL OR (upstream_status != '%s' AND upstream_status != '%s' AND upstream_status != '%s'))",
		events.AccountStatusDeactivated, events.AccountStatusSuspended, events.AccountStatusTakendown)

	// Load the users
	users := []*User{}
	if err := s.db.Model(&User{}).Where(q, cursor).Order("id").Limit(limit).Find(&users).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			return &comatprototypes.SyncListRepos_Output{}, nil
		}
		log.Error("failed to query users", "err", err)
		return nil, echo.NewHTTPError(http.StatusInternalServerError, "failed to query users")
	}

	if len(users) == 0 {
		// resp.Repos is an explicit empty array, not just 'nil'
		return &comatprototypes.SyncListRepos_Output{
			Repos: []*comatprototypes.SyncListRepos_Repo{},
		}, nil
	}

	resp := &comatprototypes.SyncListRepos_Output{
		Repos: make([]*comatprototypes.SyncListRepos_Repo, len(users)),
	}

	// Fetch the repo roots for each user
	for i := range users {
		user := users[i]

		root, err := s.repoman.GetRepoRoot(ctx, user.ID)
		if err != nil {
			log.Error("failed to get repo root", "err", err, "did", user.Did)
			return nil, echo.NewHTTPError(http.StatusInternalServerError, fmt.Sprintf("failed to get repo root for (%s): %v", user.Did, err.Error()))
		}

		resp.Repos[i] = &comatprototypes.SyncListRepos_Repo{
			Did:  user.Did,
			Head: root.String(),
		}
	}

	// If this is not the last page, set the cursor
	if len(users) >= limit && len(users) > 1 {
		nextCursor := fmt.Sprintf("%d", users[len(users)-1].ID)
		resp.Cursor = &nextCursor
	}

	return resp, nil
}

func (s *BGS) handleComAtprotoSyncGetLatestCommit(ctx context.Context, did string) (*comatprototypes.SyncGetLatestCommit_Output, error) {
	u, err := s.lookupUserByDid(ctx, did)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, echo.NewHTTPError(http.StatusNotFound, "user not found")
		}
		return nil, echo.NewHTTPError(http.StatusInternalServerError, "failed to lookup user")
	}

	if u.GetTombstoned() {
		return nil, fmt.Errorf("account was deleted")
	}

	if u.GetTakenDown() {
		return nil, fmt.Errorf("account was taken down by the Relay")
	}

	ustatus := u.GetUpstreamStatus()
	if ustatus == events.AccountStatusTakendown {
		return nil, fmt.Errorf("account was taken down by its PDS")
	}

	if ustatus == events.AccountStatusDeactivated {
		return nil, fmt.Errorf("account is temporarily deactivated")
	}

	if ustatus == events.AccountStatusSuspended {
		return nil, fmt.Errorf("account is suspended by its PDS")
	}

	root, err := s.repoman.GetRepoRoot(ctx, u.ID)
	if err != nil {
		log.Error("failed to get repo root", "err", err, "did", u.Did)
		return nil, echo.NewHTTPError(http.StatusInternalServerError, "failed to get repo root")
	}

	rev, err := s.repoman.GetRepoRev(ctx, u.ID)
	if err != nil {
		log.Error("failed to get repo rev", "err", err, "did", u.Did)
		return nil, echo.NewHTTPError(http.StatusInternalServerError, "failed to get repo rev")
	}

	return &comatprototypes.SyncGetLatestCommit_Output{
		Cid: root.String(),
		Rev: rev,
	}, nil
}
