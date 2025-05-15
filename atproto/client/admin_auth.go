package client

import (
	"encoding/base64"
	"net/http"
)

type AdminAuth struct {
	basicAuthHeader string
}

func NewAdminAuth(password string) AdminAuth {
	header := "Basic" + base64.StdEncoding.EncodeToString([]byte("admin:"+password))
	return AdminAuth{basicAuthHeader: header}
}

func (a *AdminAuth) DoWithAuth(c *http.Client, req *http.Request) (*http.Response, error) {
	req.Header.Set("Authorization", a.basicAuthHeader)
	return c.Do(req)
}
