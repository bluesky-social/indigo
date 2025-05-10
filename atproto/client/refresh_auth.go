package client

import (
	"context"
	"fmt"
	"net/http"
	"sync"

	comatproto "github.com/bluesky-social/indigo/api/atproto"

	"github.com/bluesky-social/indigo/atproto/syntax"
)

type RefreshAuth struct {
	AccessToken  string
	RefreshToken string
	DID          syntax.DID
	// The AuthHost might different from any APIClient host, if there is an entryway involved
	AuthHost string

	lk sync.Mutex
}

// TODO:
//func NewRefreshAuth(pdsHost, accountIdentifier, password string) (*RefreshAuth, error) {

func (a *RefreshAuth) DoWithAuth(req *http.Request, c *http.Client) (*http.Response, error) {
	req.Header.Set("Authorization", "Bearer "+a.AccessToken)
	// XXX: check response. if it is 403, because access token is expired, then take a lock and do a refresh
	// TODO: when doing a refresh request, copy at least the User-Agent header from httpReq, and re-use httpClient
	return c.Do(httpReq)
}

// updates the client with the new auth method
func NewSession(ctx context.Context, client *APIClient, username, password, token string) (*RefreshAuth, error) {

	reqBody := comatproto.ServerCreateSession_Input{
		Identifier: username,
		Password:   password,
	}
	if token != "" {
		reqBody.AuthFactorToken = &token
	}

	var out comatproto.ServerCreateSession_Output
	err := client.Post(ctx, syntax.NSID("com.atproto.server.createSession"), &reqBody, &out)
	if err != nil {
		return nil, err
	}

	if out.Active != nil && *out.Active == false {
		return nil, fmt.Errorf("account is disabled: %s", out.Status)
	}

	ra := RefreshAuth{
		AccessToken:  out.AccessJwt,
		RefreshToken: out.RefreshJwt,
		DID:          syntax.DID(out.Did),
		// TODO: authHost / PDS host distinction
		AuthHost: client.Host,
	}
	client.Auth = &ra
	return &ra, nil
}
