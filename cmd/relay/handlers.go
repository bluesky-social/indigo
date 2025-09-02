package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"

	comatproto "github.com/bluesky-social/indigo/api/atproto"
	"github.com/bluesky-social/indigo/atproto/syntax"
	"github.com/bluesky-social/indigo/cmd/relay/relay"
	"github.com/bluesky-social/indigo/cmd/relay/relay/models"
	"github.com/bluesky-social/indigo/xrpc"

	"github.com/labstack/echo/v4"
)

func (s *Service) handleComAtprotoSyncRequestCrawl(c echo.Context, body *comatproto.SyncRequestCrawl_Input, admin bool) error {
	ctx := c.Request().Context()

	if s.config.DisableRequestCrawl && !admin {
		return c.JSON(http.StatusForbidden, xrpc.XRPCError{ErrStr: "Forbidden", Message: "public requestCrawl not allowed on this relay"})
	}

	hostname, noSSL, err := relay.ParseHostname(body.Hostname)
	if err != nil {
		return c.JSON(http.StatusBadRequest, xrpc.XRPCError{ErrStr: "BadRequest", Message: fmt.Sprintf("hostname field empty or invalid: %s", body.Hostname)})
	}

	if noSSL && !s.config.AllowInsecureHosts && !admin {
		return c.JSON(http.StatusBadRequest, xrpc.XRPCError{ErrStr: "BadRequest", Message: "this relay requires host SSL"})
	}

	// TODO: could ensure that query and path are empty

	if strings.HasPrefix(hostname, "localhost:") {
		if !admin {
			return c.JSON(http.StatusBadRequest, xrpc.XRPCError{ErrStr: "BadRequest", Message: "can not configure localhost via public endpoint"})
		}
		// else, allowed
	} else {
		banned, err := s.relay.DomainIsBanned(ctx, hostname)
		if err != nil {
			return nil
		}
		if banned {
			return c.JSON(http.StatusUnauthorized, xrpc.XRPCError{ErrStr: "DomainBan", Message: "host domain is banned"})
		}
	}

	hostURL := "https://" + hostname
	if noSSL {
		hostURL = "http://" + hostname
	}

	if err := s.relay.HostChecker.CheckHost(ctx, hostURL); err != nil {
		return c.JSON(http.StatusBadRequest, xrpc.XRPCError{ErrStr: "HostNotFound", Message: fmt.Sprintf("host server unreachable: %s", err)})
	}

	// forward on to any sibling instances (note that sometimes is, sometimes isn't an admin request)
	b, err := json.Marshal(body)
	if err != nil {
		return err
	}
	go s.ForwardSiblingRequest(c, b)

	return s.relay.SubscribeToHost(ctx, hostname, noSSL, admin)
}

func (s *Service) handleComAtprotoSyncListHosts(c echo.Context, cursor int64, limit int) (*comatproto.SyncListHosts_Output, error) {
	ctx := c.Request().Context()

	hosts, err := s.relay.ListHosts(ctx, cursor, limit, true)
	if err != nil {
		return nil, c.JSON(http.StatusInternalServerError, xrpc.XRPCError{ErrStr: "DatabaseError", Message: "failed to list hosts"})
	}

	if len(hosts) == 0 {
		// resp.Hosts is an explicit empty array, not just 'nil'
		return &comatproto.SyncListHosts_Output{
			Hosts: []*comatproto.SyncListHosts_Host{},
		}, nil
	}

	resp := &comatproto.SyncListHosts_Output{
		Hosts: make([]*comatproto.SyncListHosts_Host, len(hosts)),
	}

	for i, host := range hosts {
		resp.Hosts[i] = &comatproto.SyncListHosts_Host{
			Hostname:     host.Hostname,
			Seq:          &host.LastSeq,
			Status:       (*string)(&host.Status),
			AccountCount: &host.AccountCount,
		}
	}

	// If this is not the last page, set the cursor
	if len(hosts) >= limit && len(hosts) > 1 {
		nextCursor := fmt.Sprintf("%d", hosts[len(hosts)-1].ID)
		resp.Cursor = &nextCursor
	}

	return resp, nil
}

func (s *Service) handleComAtprotoSyncGetHostStatus(c echo.Context, hostname string) (*comatproto.SyncGetHostStatus_Output, error) {
	ctx := c.Request().Context()

	host, err := s.relay.GetHost(ctx, hostname)
	if err != nil {
		if errors.Is(err, relay.ErrHostNotFound) {
			// TODO: test that not found DID is a 404
			return nil, c.JSON(http.StatusNotFound, xrpc.XRPCError{ErrStr: "HostNotFound", Message: "host not found"})
		}
		return nil, c.JSON(http.StatusInternalServerError, xrpc.XRPCError{ErrStr: "DatabaseError", Message: "looking up host information"})
	}

	out := &comatproto.SyncGetHostStatus_Output{
		// TODO: AccountCount
		Hostname: host.Hostname,
		Seq:      &host.LastSeq,
		Status:   (*string)(&host.Status),
	}

	return out, nil
}

func (s *Service) handleComAtprotoSyncListRepos(c echo.Context, cursor int64, limit int) (*comatproto.SyncListRepos_Output, error) {
	ctx := c.Request().Context()

	accounts, err := s.relay.ListAccounts(ctx, cursor, limit)
	if err != nil {
		s.logger.Error("failed to query accounts", "err", err)
		return nil, c.JSON(http.StatusInternalServerError, xrpc.XRPCError{ErrStr: "DatabaseError", Message: "failed to list accounts (repos)"})
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
	// TODO: would be much more efficient to do a join and have Relay.ListAccounts return these repos with the account info
	for i, acc := range accounts {
		repo, err := s.relay.GetAccountRepo(ctx, acc.UID)
		if err != nil && !errors.Is(err, relay.ErrAccountRepoNotFound) {
			s.logger.Error("failed to get repo root", "err", err, "did", acc.DID)
			return nil, echo.NewHTTPError(http.StatusInternalServerError, fmt.Sprintf("failed to get repo root for (%s): %v", acc.DID, err.Error()))
		}
		// note: repo can be nil beyond this point!

		// TODO: empty strings here may not be spec-compliant
		// need to determine correct handling when there is no account_repo entry
		// see https://github.com/bluesky-social/indigo/issues/1143
		var cid, rev string
		if repo != nil {
			cid = repo.CommitCID
			rev = repo.Rev
		}

		active := acc.IsActive()
		resp.Repos[i] = &comatproto.SyncListRepos_Repo{
			Did:    acc.DID,
			Head:   cid,
			Rev:    rev,
			Active: &active,
			Status: acc.StatusField(),
		}
	}

	// If this is not the last page, set the cursor
	if len(accounts) >= limit && len(accounts) > 1 {
		nextCursor := fmt.Sprintf("%d", accounts[len(accounts)-1].UID)
		resp.Cursor = &nextCursor
	}

	return resp, nil
}

func (s *Service) handleComAtprotoSyncGetRepoStatus(c echo.Context, did syntax.DID) (*comatproto.SyncGetRepoStatus_Output, error) {
	ctx := c.Request().Context()

	acc, err := s.relay.GetAccount(ctx, did)
	if err != nil {
		if errors.Is(err, relay.ErrAccountNotFound) {
			// TODO: test that not found DID is a 404
			return nil, c.JSON(http.StatusNotFound, xrpc.XRPCError{ErrStr: "RepoNotFound", Message: "account not found"})
		}
		return nil, c.JSON(http.StatusInternalServerError, xrpc.XRPCError{ErrStr: "DatabaseError", Message: "looking up account information"})
	}

	out := &comatproto.SyncGetRepoStatus_Output{
		Did:    did.String(),
		Active: acc.IsActive(),
		Status: acc.StatusField(),
	}

	repo, err := s.relay.GetAccountRepo(ctx, acc.UID)
	if err != nil && !errors.Is(err, relay.ErrAccountRepoNotFound) {
		return nil, err
	}
	// ^^ only returns for non-ErrAccountRepoNotFound: repo can be nil after this!

	if repo != nil {
		out.Rev = &repo.Rev
	}

	return out, nil
}

func (s *Service) handleComAtprotoSyncGetLatestCommit(c echo.Context, did syntax.DID) (*comatproto.SyncGetLatestCommit_Output, error) {
	ctx := c.Request().Context()

	acc, err := s.relay.GetAccount(ctx, did)
	if err != nil {
		if errors.Is(err, relay.ErrAccountNotFound) {
			// TODO: test that not found DID is a 404
			return nil, c.JSON(http.StatusNotFound, xrpc.XRPCError{ErrStr: "RepoNotFound", Message: "account not found"})
		}
		return nil, c.JSON(http.StatusInternalServerError, xrpc.XRPCError{ErrStr: "DatabaseError", Message: "looking up account information"})
	}

	switch acc.AccountStatus() {
	case models.AccountStatusTakendown, models.AccountStatusSuspended:
		return nil, c.JSON(http.StatusForbidden, xrpc.XRPCError{ErrStr: "RepoTakendown", Message: "account not active (takendown)"})
	case models.AccountStatusDeactivated:
		return nil, c.JSON(http.StatusForbidden, xrpc.XRPCError{ErrStr: "RepoDeactivated", Message: "account not active (deactivated)"})
	case models.AccountStatusDeleted:
		return nil, c.JSON(http.StatusForbidden, xrpc.XRPCError{ErrStr: "RepoDeleted", Message: "account not active (deleted)"})
	case models.AccountStatusActive:
		// pass
	default:
		return nil, c.JSON(http.StatusForbidden, xrpc.XRPCError{ErrStr: "RepoInactive", Message: fmt.Sprintf("account not active: %s", acc.AccountStatus())})
	}

	repo, err := s.relay.GetAccountRepo(ctx, acc.UID)
	if err != nil {
		if errors.Is(err, relay.ErrAccountRepoNotFound) {
			return nil, c.JSON(http.StatusNotFound, xrpc.XRPCError{ErrStr: "RepoNotSynchronized", Message: "do not know current repo state for account"})
		}
		return nil, err
	}

	return &comatproto.SyncGetLatestCommit_Output{
		Cid: repo.CommitCID,
		Rev: repo.Rev,
	}, nil
}

type HealthStatus struct {
	Status  string `json:"status"`
	Message string `json:"msg,omitempty"`
}

func (svc *Service) HandleHealthCheck(c echo.Context) error {
	ctx := c.Request().Context()
	if err := svc.relay.Healthcheck(ctx); err != nil {
		svc.logger.Error("healthcheck can't connect to database", "err", err)
		return c.JSON(http.StatusInternalServerError, HealthStatus{Status: "error", Message: "can't connect to database"})
	} else {
		return c.JSON(http.StatusOK, HealthStatus{Status: "ok"})
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

This is an atproto [https://atproto.com] relay instance, running the 'relay' codebase [https://github.com/bluesky-social/indigo]

The firehose WebSocket path is at:  /xrpc/com.atproto.sync.subscribeRepos
`

func (svc *Service) HandleHomeMessage(c echo.Context) error {
	return c.String(http.StatusOK, homeMessage)
}
