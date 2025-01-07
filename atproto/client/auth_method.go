package client

import (
	"context"
	"net/http"

	"github.com/bluesky-social/indigo/atproto/syntax"
)

type AuthMethod interface {
	DoWithAuth(ctx context.Context, httpReq *http.Request, httpClient *http.Client) (*http.Response, error)
	AccountDID() syntax.DID
}
