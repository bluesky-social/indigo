package client

import (
	"context"
	"net/http"

	"github.com/bluesky-social/indigo/atproto/syntax"
)

type RefreshAuth struct {
	AccessToken  string
	RefreshToken string
	DID          syntax.DID
	// The AuthHost might different from any APIClient host, if there is an entryway involved
	AuthHost string
}

// TODO:
//func NewRefreshAuth(pdsHost, accountIdentifier, password string) (*RefreshAuth, error) {

func (a *RefreshAuth) DoWithAuth(ctx context.Context, httpReq *http.Request, httpClient *http.Client) (*http.Response, error) {
	httpReq.Header.Set("Authorization", "Bearer "+a.AccessToken)
	// XXX: check response. if it is 403, because access token is expired, then take a lock and do a refresh
	// TODO: when doing a refresh request, copy at least the User-Agent header from httpReq, and re-use httpClient
	return httpClient.Do(httpReq)
}

// Admin bearer token auth does not involve an account DID
func (a *RefreshAuth) AccountDID() syntax.DID {
	return a.DID
}
