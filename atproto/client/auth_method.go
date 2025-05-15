package client

import (
	"net/http"
)

type AuthMethod interface {
	DoWithAuth(c *http.Client, req *http.Request) (*http.Response, error)
}
