package atclient

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"sync"

	"github.com/bluesky-social/indigo/atproto/identity"
	"github.com/bluesky-social/indigo/atproto/syntax"
)

type RefreshCallback = func(ctx context.Context, data PasswordSessionData)

// Implementation of [AuthMethod] for password-based auth sessions with atproto PDS hosts. Automatically refreshes "access token" using a "refresh token" when needed.
//
// It is safe to use this auth method concurrently from multiple goroutines.
type PasswordAuth struct {
	Session PasswordSessionData

	// Optional callback function which gets called with updated session data whenever a successful token refresh happens.
	//
	// Note that this function is called while a lock is being held on the overall client, and with a context usually tied to a regular API request call. The callback should either return quickly, or spawn a goroutine. Because of the lock, this callback will never be called concurrently for a single client, but may be called currently across clients.
	RefreshCallback RefreshCallback

	// Lock which protects concurrent access to AccessToken and RefreshToken in session data. Note that this only applies to this particular instance of PasswordAuth.
	lk sync.RWMutex
}

// Data about a PDS password auth session which can be persisted and then used to resume the session later.
type PasswordSessionData struct {
	AccessToken  string     `json:"access_token"`
	RefreshToken string     `json:"refresh_token"`
	AccountDID   syntax.DID `json:"account_did"`
	Host         string     `json:"host"`
}

// Creates a deep copy of the session data.
func (sd *PasswordSessionData) Clone() PasswordSessionData {
	return PasswordSessionData{
		AccessToken:  sd.AccessToken,
		RefreshToken: sd.RefreshToken,
		AccountDID:   sd.AccountDID,
		Host:         sd.Host,
	}
}

type createSessionRequest struct {
	//AllowTakendown  *bool   `json:"allowTakendown,omitempty" cborgen:"allowTakendown,omitempty"`
	AuthFactorToken *string `json:"authFactorToken,omitempty" cborgen:"authFactorToken,omitempty"`
	// identifier: Handle or other identifier supported by the server for the authenticating user.
	Identifier string `json:"identifier" cborgen:"identifier"`
	Password   string `json:"password" cborgen:"password"`
}

type createSessionResponse struct {
	AccessJwt string `json:"accessJwt" cborgen:"accessJwt"`
	Active    *bool  `json:"active,omitempty" cborgen:"active,omitempty"`
	Did       string `json:"did" cborgen:"did"`
	//Email           *string      `json:"email,omitempty" cborgen:"email,omitempty"`
	//EmailAuthFactor *bool        `json:"emailAuthFactor,omitempty" cborgen:"emailAuthFactor,omitempty"`
	//EmailConfirmed  *bool        `json:"emailConfirmed,omitempty" cborgen:"emailConfirmed,omitempty"`
	//Handle          string       `json:"handle" cborgen:"handle"`
	RefreshJwt string  `json:"refreshJwt" cborgen:"refreshJwt"`
	Status     *string `json:"status,omitempty" cborgen:"status,omitempty"`
}

type refreshSessionResponse struct {
	AccessJwt string `json:"accessJwt" cborgen:"accessJwt"`
	Active    *bool  `json:"active,omitempty" cborgen:"active,omitempty"`
	Did       string `json:"did" cborgen:"did"`
	//Handle     string       `json:"handle" cborgen:"handle"`
	RefreshJwt string  `json:"refreshJwt" cborgen:"refreshJwt"`
	Status     *string `json:"status,omitempty" cborgen:"status,omitempty"`
}

func (a *PasswordAuth) DoWithAuth(c *http.Client, req *http.Request, endpoint syntax.NSID) (*http.Response, error) {
	accessToken, refreshToken := a.GetTokens()
	req.Header.Set("Authorization", "Bearer "+accessToken)
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
	if err := a.Refresh(req.Context(), c, refreshToken); err != nil {
		return nil, err
	}

	retry := req.Clone(req.Context())
	if req.GetBody != nil {
		retry.Body, err = req.GetBody()
		if err != nil {
			return nil, fmt.Errorf("API request retry GetBody failed: %w", err)
		}
	}

	accessToken, _ = a.GetTokens()

	retry.Header.Set("Authorization", "Bearer "+accessToken)
	retryResp, err := c.Do(retry)
	if err != nil {
		return nil, err
	}
	// NOTE: could handle auth failure as special error type here
	return retryResp, err
}

// Returns current access and refresh tokens (take a read-lock on session data)
func (a *PasswordAuth) GetTokens() (string, string) {
	a.lk.RLock()
	defer a.lk.RUnlock()
	return a.Session.AccessToken, a.Session.RefreshToken
}

// Refreshes auth tokens (takes a write-lock on session data).
//
// `priorRefreshToken` argument is used to check if a concurrent refresh already took place.
func (a *PasswordAuth) Refresh(ctx context.Context, c *http.Client, priorRefreshToken string) error {

	a.lk.Lock()
	defer a.lk.Unlock()

	// basic concurrency check: if refresh token already changed, can bail here (releasing lock)
	if priorRefreshToken != "" && priorRefreshToken != a.Session.RefreshToken {
		return nil
	}

	u := a.Session.Host + "/xrpc/com.atproto.server.refreshSession"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return err
	}
	// NOTE: could try to pull User-Agent from a request and pass that through to here
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
		// TODO: indicate in the error that it was from refresh process, not original request?
		return eb.APIError(resp.StatusCode)
	}

	var out refreshSessionResponse
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return err
	}

	a.Session.AccessToken = out.AccessJwt
	a.Session.RefreshToken = out.RefreshJwt

	if a.RefreshCallback != nil {
		snapshot := a.Session.Clone()
		a.RefreshCallback(ctx, snapshot)
	}

	return nil
}

func (a *PasswordAuth) Logout(ctx context.Context, c *http.Client) error {
	_, refreshToken := a.GetTokens()

	u := a.Session.Host + "/xrpc/com.atproto.server.deleteSession"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, u, nil)
	if err != nil {
		return err
	}
	// NOTE: could try to pull User-Agent from a request and pass that through to here
	req.Header.Set("User-Agent", "indigo-sdk")

	// NOTE: using refresh token here, not access token
	req.Header.Set("Authorization", "Bearer "+refreshToken)

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
		return eb.APIError(resp.StatusCode)
	}
	return nil
}

// Creates a new [APIClient] with [PasswordAuth] for the provided user. The provided identity directory is used to resolve the PDS host for the account.
//
// `authToken` is optional; is used when multi-factor authentication is enabled for the account.
//
// `cb` is an optional callback which will be called with updated session data after any token refresh.
func LoginWithPassword(ctx context.Context, dir identity.Directory, username syntax.AtIdentifier, password, authToken string, cb RefreshCallback) (*APIClient, error) {

	ident, err := dir.Lookup(ctx, username)
	if err != nil {
		return nil, err
	}

	host := ident.PDSEndpoint()
	if host == "" {
		return nil, fmt.Errorf("account does not have PDS registered")
	}

	c, err := LoginWithPasswordHost(ctx, host, ident.DID.String(), password, authToken, cb)
	if err != nil {
		return nil, err
	}

	if c.AccountDID == nil || *c.AccountDID != ident.DID {
		return nil, fmt.Errorf("returned session DID not requested account: %s", c.AccountDID)
	}

	return c, nil
}

// Creates a new [APIClient] with [PasswordAuth], based on a login to the provided host. Note that with some PDS implementations, 'username' could be an email address. This login method also works in situations where an account's network identity does not resolve to this specific host.
//
// `authToken` is optional; is used when multi-factor authentication is enabled for the account.
//
// `cb` is an optional callback which will be called with updated session data after any token refresh.
func LoginWithPasswordHost(ctx context.Context, host, username, password, authToken string, cb RefreshCallback) (*APIClient, error) {

	c := NewAPIClient(host)
	reqBody := createSessionRequest{
		Identifier: username,
		Password:   password,
	}
	if authToken != "" {
		reqBody.AuthFactorToken = &authToken
	}

	var out createSessionResponse
	if err := c.Post(ctx, syntax.NSID("com.atproto.server.createSession"), &reqBody, &out); err != nil {
		return nil, err
	}

	if out.Active != nil && *out.Active == false {
		return nil, fmt.Errorf("account is disabled: %v", out.Status)
	}

	did, err := syntax.ParseDID(out.Did)
	if err != nil {
		return nil, err
	}

	ra := PasswordAuth{
		Session: PasswordSessionData{
			AccessToken:  out.AccessJwt,
			RefreshToken: out.RefreshJwt,
			AccountDID:   did,
			Host:         c.Host,
		},
		RefreshCallback: cb,
	}
	c.Auth = &ra
	c.AccountDID = &did
	return c, nil
}

// Creates an [APIClient] using [PasswordAuth], based on existing session data.
//
// `cb` is an optional callback which will be called with updated session data after any token refresh.
func ResumePasswordSession(data PasswordSessionData, cb RefreshCallback) *APIClient {
	c := NewAPIClient(data.Host)
	ra := PasswordAuth{
		Session:         data,
		RefreshCallback: cb,
	}
	c.Auth = &ra
	c.AccountDID = &data.AccountDID
	return c
}
