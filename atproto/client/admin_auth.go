package client

import (
	"net/http"
)

type AdminAuth struct {
	Password string
}

func NewAdminAuth(password string) AdminAuth {
	return AdminAuth{Password: password}
}

func (a *AdminAuth) DoWithAuth(c *http.Client, req *http.Request) (*http.Response, error) {
	req.SetBasicAuth("admin", a.Password)
	return c.Do(req)
}
