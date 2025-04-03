package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"

	comatproto "github.com/bluesky-social/indigo/api/atproto"
	"github.com/bluesky-social/indigo/atproto/syntax"
	"github.com/bluesky-social/indigo/cmd/relayered/relay/models"
	"github.com/bluesky-social/indigo/xrpc"

	"github.com/labstack/echo/v4"
	"gorm.io/gorm"
)

func (s *Service) handleComAtprotoSyncRequestCrawl(ctx context.Context, body *comatproto.SyncRequestCrawl_Input) error {
	host := body.Hostname
	if host == "" {
		return echo.NewHTTPError(http.StatusBadRequest, "must pass hostname")
	}

	if !strings.HasPrefix(host, "http://") && !strings.HasPrefix(host, "https://") {
		if s.relay.Config.SSL {
			host = "https://" + host
		} else {
			host = "http://" + host
		}
	}

	u, err := url.Parse(host)
	if err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, "failed to parse hostname")
	}

	if u.Scheme == "http" && s.relay.Config.SSL {
		return echo.NewHTTPError(http.StatusBadRequest, "this server requires https")
	}

	if u.Scheme == "https" && !s.relay.Config.SSL {
		return echo.NewHTTPError(http.StatusBadRequest, "this server does not support https")
	}

	if u.Path != "" {
		return echo.NewHTTPError(http.StatusBadRequest, "must pass hostname without path")
	}

	if u.Query().Encode() != "" {
		return echo.NewHTTPError(http.StatusBadRequest, "must pass hostname without query")
	}

	host = u.Host // potentially hostname:port

	banned, err := s.relay.DomainIsBanned(ctx, host)
	if banned {
		return echo.NewHTTPError(http.StatusUnauthorized, "domain is banned")
	}

	s.logger.Warn("TODO: better host validation for crawl requests")

	clientHost := fmt.Sprintf("%s://%s", u.Scheme, host)

	c := &xrpc.Client{
		Host:   clientHost,
		Client: http.DefaultClient, // not using the client that auto-retries
	}

	desc, err := comatproto.ServerDescribeServer(ctx, c)
	if err != nil {
		errMsg := fmt.Sprintf("requested host (%s) failed to respond to describe request", clientHost)
		return echo.NewHTTPError(http.StatusBadRequest, errMsg)
	}

	// Maybe we could do something with this response later
	_ = desc

	if len(s.config.NextCrawlers) != 0 {
		blob, err := json.Marshal(body)
		if err != nil {
			s.logger.Warn("could not forward requestCrawl, json err", "err", err)
		} else {
			go func(bodyBlob []byte) {
				for _, rpu := range s.config.NextCrawlers {
					pu := rpu.JoinPath("/xrpc/com.atproto.sync.requestCrawl")
					response, err := s.crawlForwardClient.Post(pu.String(), "application/json", bytes.NewReader(bodyBlob))
					if response != nil && response.Body != nil {
						response.Body.Close()
					}
					if err != nil || response == nil {
						s.logger.Warn("requestCrawl forward failed", "host", rpu, "err", err)
					} else if response.StatusCode != http.StatusOK {
						s.logger.Warn("requestCrawl forward failed", "host", rpu, "status", response.Status)
					} else {
						s.logger.Info("requestCrawl forward successful", "host", rpu)
					}
				}
			}(blob)
		}
	}

	return s.relay.Slurper.SubscribeToPds(ctx, host, true, false, nil)
}

func (s *Service) handleComAtprotoSyncListRepos(ctx context.Context, cursor int64, limit int) (*comatproto.SyncListRepos_Output, error) {
	accounts, err := s.relay.ListAccounts(ctx, cursor, limit)
	if err != nil {
		if err == gorm.ErrRecordNotFound {
			return &comatproto.SyncListRepos_Output{}, nil
		}
		s.logger.Error("failed to query accounts", "err", err)
		return nil, echo.NewHTTPError(http.StatusInternalServerError, "failed to query accounts")
	}

	if len(accounts) == 0 {
		// resp.Repos is an explicit empty array, not just 'nil'
		return &comatproto.SyncListRepos_Output{
			Repos: []*comatproto.SyncListRepos_Repo{},
		}, nil
	}

	resp := &comatproto.SyncListRepos_Output{
		Repos: make([]*comatproto.SyncListRepos_Repo, len(accounts)),
	}

	// Fetch the repo roots for each user
	for i, acc := range accounts {
		repo, err := s.relay.GetAccountRepo(ctx, acc.UID)
		if err != nil {
			s.logger.Error("failed to get repo root", "err", err, "did", acc.DID)
			return nil, echo.NewHTTPError(http.StatusInternalServerError, fmt.Sprintf("failed to get repo root for (%s): %v", acc.DID, err.Error()))
		}

		resp.Repos[i] = &comatproto.SyncListRepos_Repo{
			Did:  acc.DID,
			Head: repo.CommitData, // XXX: is this what is expected here?
		}
	}

	// If this is not the last page, set the cursor
	if len(accounts) >= limit && len(accounts) > 1 {
		nextCursor := fmt.Sprintf("%d", accounts[len(accounts)-1].UID)
		resp.Cursor = &nextCursor
	}

	return resp, nil
}

func (s *Service) handleComAtprotoSyncGetLatestCommit(ctx context.Context, rawDID string) (*comatproto.SyncGetLatestCommit_Output, error) {
	did, err := syntax.ParseDID(rawDID)
	if err != nil {
		return nil, fmt.Errorf("invalid DID parameter: %w", err)
	}
	acc, err := s.relay.GetAccount(ctx, did)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, echo.NewHTTPError(http.StatusNotFound, "user not found")
		}
		return nil, echo.NewHTTPError(http.StatusInternalServerError, "failed to lookup user")
	}

	if acc.Status == models.AccountStatusTakendown {
		return nil, fmt.Errorf("account was taken down by the Relay")
	}

	if acc.UpstreamStatus == models.AccountStatusTakendown {
		return nil, fmt.Errorf("account was taken down by its PDS")
	}

	if acc.Status == models.AccountStatusDeactivated {
		return nil, fmt.Errorf("account is temporarily deactivated")
	}

	if acc.Status == models.AccountStatusSuspended {
		return nil, fmt.Errorf("account is suspended by its PDS")
	}

	repo, err := s.relay.GetAccountRepo(ctx, acc.UID)
	if err != nil {
		return nil, err
	}

	return &comatproto.SyncGetLatestCommit_Output{
		Cid: repo.CommitData, // XXX: this is probably not what is wanted here
		Rev: repo.Rev,
	}, nil
}

type HealthStatus struct {
	Status  string `json:"status"`
	Message string `json:"msg,omitempty"`
}

func (svc *Service) HandleHealthCheck(c echo.Context) error {
	if err := svc.relay.Healthcheck(); err != nil {
		svc.logger.Error("healthcheck can't connect to database", "err", err)
		return c.JSON(500, HealthStatus{Status: "error", Message: "can't connect to database"})
	} else {
		return c.JSON(200, HealthStatus{Status: "ok"})
	}
}

var homeMessage string = `
.########..########.##..........###....##....##
.##.....##.##.......##.........##.##....##..##.
.##.....##.##.......##........##...##....####..
.########..######...##.......##.....##....##...
.##...##...##.......##.......#########....##...
.##....##..##.......##.......##.....##....##...
.##.....##.########.########.##.....##....##...

This is an atproto [https://atproto.com] relay instance, running the 'relayered' codebase [https://github.com/bluesky-social/indigo]

The firehose WebSocket path is at:  /xrpc/com.atproto.sync.subscribeRepos
`

func (svc *Service) HandleHomeMessage(c echo.Context) error {
	return c.String(http.StatusOK, homeMessage)
}
