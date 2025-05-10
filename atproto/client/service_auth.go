package client

import (
	"net/http"
	"time"

	"github.com/bluesky-social/indigo/atproto/crypto"
	"github.com/bluesky-social/indigo/atproto/syntax"
)

// used for inter-service requests, using JWTs
type ServiceAuth struct {
	// account DID
	Issuer syntax.DID
	// optionally, service context
	IssuerFrag string
	Duration   time.Duration
	SigningKey *crypto.PrivateKey
}

func NewServiceAuth(issuer syntax.DID, frag string, key *crypto.PrivateKey) ServiceAuth {
	return ServiceAuth{
		Issuer:     issuer,
		IssuerFrag: frag,
		Duration:   time.Second * 30,
		SigningKey: key,
	}
}

func (a *ServiceAuth) DoWithAuth(req *http.Request, c *http.Client) (*http.Response, error) {
	// TODO: detect audience from request headers (atproto-proxy)
	// TODO: extract endpoint (LXM) from request

	thing := ""
	req.Header.Set("Authorization", "Bearer "+thing)
	return c.Do(req)
}
