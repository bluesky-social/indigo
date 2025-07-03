package client

import (
	"net/http"

	"github.com/gander-social/gander-indigo-sovereign/atproto/syntax"
)

// Simple [AuthMethod] implementation for atproto "admin auth".
type AdminAuth struct {
	Password string
}

func (a *AdminAuth) DoWithAuth(c *http.Client, req *http.Request, endpoint syntax.NSID) (*http.Response, error) {
	req.SetBasicAuth("admin", a.Password)
	return c.Do(req)
}

func NewAdminClient(host, password string) *APIClient {
	c := NewAPIClient(host)
	c.Auth = &AdminAuth{Password: password}
	return c
}
