package client

import (
	"net/http"

	"github.com/bluesky-social/indigo/atproto/syntax"
)

type AuthMethod interface {
	DoWithAuth(httpReq *http.Request, httpClient *http.Client) (*http.Response, error)
	AccountDID() syntax.DID
}
