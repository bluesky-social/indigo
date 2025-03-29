package client

import (
	"context"
	"io"
	"net/http"
	"net/url"

	"github.com/bluesky-social/indigo/atproto/syntax"
)

type APIRequest struct {
	HTTPVerb    string // TODO: type?
	Endpoint    syntax.NSID
	Body        io.Reader
	QueryParams map[string]string // TODO: better type for this?
	Headers     map[string]string
}

func (r *APIRequest) HTTPRequest(ctx context.Context, host string, headers map[string]string) (*http.Request, error) {
	u, err := url.Parse(host)
	if err != nil {
		return nil, err
	}
	u.Path = "/xrpc/" + r.Endpoint.String()
	if r.QueryParams != nil {
		q := u.Query()
		for k, v := range r.QueryParams {
			q.Add(k, v)
		}
		u.RawQuery = q.Encode()
	}
	httpReq, err := http.NewRequestWithContext(ctx, r.HTTPVerb, u.String(), r.Body)
	if err != nil {
		return nil, err
	}

	// first set default headers...
	if headers != nil {
		for k, v := range headers {
			httpReq.Header.Set(k, v)
		}
	}

	// ... then request-specific take priority (overwrite)
	if r.Headers != nil {
		for k, v := range r.Headers {
			httpReq.Header.Set(k, v)
		}
	}

	return httpReq, nil
}
