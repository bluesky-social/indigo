package client

import (
	"net/http"
)

type AuthMethod interface {
	DoWithAuth(req *http.Request, c *http.Client) (*http.Response, error)
}
