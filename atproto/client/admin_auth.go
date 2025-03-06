package client

import (
	"encoding/base64"
	"net/http"

	"github.com/bluesky-social/indigo/atproto/syntax"
)

type AdminAuth struct {
	basicAuthHeader string
}

func NewAdminAuth(password string) AdminAuth {
	header := "Basic" + base64.StdEncoding.EncodeToString([]byte("admin:"+password))
	return AdminAuth{basicAuthHeader: header}
}

func (a *AdminAuth) DoWithAuth(req *http.Request, httpClient *http.Client) (*http.Response, error) {
	req.Header.Set("Authorization", a.basicAuthHeader)
	return httpClient.Do(req)
}

// Admin bearer token auth does not involve an account DID
func (a *AdminAuth) AccountDID() syntax.DID {
	return ""
}
