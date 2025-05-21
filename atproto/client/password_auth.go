package client

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"sync"

	comatproto "github.com/bluesky-social/indigo/api/atproto"
	"github.com/bluesky-social/indigo/atproto/identity"
	"github.com/bluesky-social/indigo/atproto/syntax"
)

type PasswordAuth struct {
	Session SessionData
	// TODO: RefreshCallback

	lk sync.Mutex
}

type SessionData struct {
	AccessToken  string
	RefreshToken string
	AccountDID   syntax.DID
	Host         string
}

func (a *PasswordAuth) DoWithAuth(c *http.Client, req *http.Request) (*http.Response, error) {
	req.Header.Set("Authorization", "Bearer "+a.Session.AccessToken)
	resp, err := c.Do(req)
	if err != nil {
		return nil, err
	}

	// on success, or most errors, just return HTTP response
	if resp.StatusCode != http.StatusBadRequest || !strings.HasPrefix(resp.Header.Get("Content-Type"), "application/json") {
		return resp, nil
	}

	// parse the error response body (JSON) and check the error name
	defer resp.Body.Close()
	var eb ErrorBody
	if err := json.NewDecoder(resp.Body).Decode(&eb); err != nil {
		return nil, &APIError{StatusCode: resp.StatusCode}
	}
	if eb.Name != "ExpiredToken" {
		return nil, eb.APIError(resp.StatusCode)
	}

	// ok, we had an expired token, try a refresh
	if err := a.Refresh(req.Context(), c); err != nil {
		return nil, err
	}

	retry := req.Clone(req.Context())
	if req.GetBody != nil {
		retry.Body, err = req.GetBody()
		if err != nil {
			return nil, fmt.Errorf("API request retry GetBody failed: %w", err)
		}
	}

	retry.Header.Set("Authorization", "Bearer "+a.Session.AccessToken)
	retryResp, err := c.Do(retry)
	if err != nil {
		return nil, err
	}
	// TODO: could handle auth failure as special error type here
	return retryResp, err
}

// TODO: need a "Logout" method as well? which takes the refresh token (not access token)
func (a *PasswordAuth) Refresh(ctx context.Context, c *http.Client) error {

	prior := a.Session.RefreshToken

	a.lk.Lock()
	defer a.lk.Unlock()

	// XXX: basic concurrency check: if refresh token already changed, can bail here. should probably handle this better (accept refresh token as input?)
	if prior != a.Session.RefreshToken {
		return nil
	}

	u := a.Session.Host + "/xrpc/com.atproto.server.refreshSession"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return err
	}
	// TODO: this doesn't inherit User-Agent header
	req.Header.Set("User-Agent", "indigo-sdk")

	// NOTE: using refresh token here, not access token
	req.Header.Set("Authorization", "Bearer "+a.Session.RefreshToken)

	resp, err := c.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if !(resp.StatusCode >= 200 && resp.StatusCode < 300) {
		var eb ErrorBody
		if err := json.NewDecoder(resp.Body).Decode(&eb); err != nil {
			return &APIError{StatusCode: resp.StatusCode}
		}
		// TODO: indicate in this error that it was from refresh process, not original request?
		return eb.APIError(resp.StatusCode)
	}

	var out comatproto.ServerRefreshSession_Output
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return err
	}

	a.Session.AccessToken = out.AccessJwt
	a.Session.RefreshToken = out.RefreshJwt
	// TODO: callback?

	return nil
}

func LoginWithPassword(ctx context.Context, dir identity.Directory, username syntax.AtIdentifier, password, authToken string) (*APIClient, error) {

	ident, err := dir.Lookup(ctx, username)
	if err != nil {
		return nil, err
	}

	host := ident.PDSEndpoint()
	if host == "" {
		return nil, fmt.Errorf("account does not have PDS registered")
	}

	c := NewAPIClient(host)
	reqBody := comatproto.ServerCreateSession_Input{
		Identifier: ident.DID.String(),
		Password:   password,
	}
	if authToken != "" {
		reqBody.AuthFactorToken = &authToken
	}

	// TODO: copy/vendor in session objects
	var out comatproto.ServerCreateSession_Output
	if err := c.Post(ctx, syntax.NSID("com.atproto.server.createSession"), &reqBody, &out); err != nil {
		return nil, err
	}

	if out.Active != nil && *out.Active == false {
		return nil, fmt.Errorf("account is disabled: %v", out.Status)
	}

	if out.Did != ident.DID.String() {
		return nil, fmt.Errorf("returned session DID not requested account: %s", out.Did)
	}

	ra := PasswordAuth{
		Session: SessionData{
			AccessToken:  out.AccessJwt,
			RefreshToken: out.RefreshJwt,
			AccountDID:   ident.DID,
			Host:         c.Host,
		},
	}
	c.Auth = &ra
	c.AccountDID = &ident.DID
	return c, nil
}
