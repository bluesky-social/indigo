package client

import (
	"net/http"
)

type AdminAuth struct {
	Password string
}

func (a *AdminAuth) DoWithAuth(c *http.Client, req *http.Request) (*http.Response, error) {
	req.SetBasicAuth("admin", a.Password)
	return c.Do(req)
}

func NewAdminClient(host, password string) *APIClient {
	c := NewPublicClient(host)
	c.Auth = &AdminAuth{Password: password}
	return c
}
